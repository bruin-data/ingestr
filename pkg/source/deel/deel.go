package deel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/sync/errgroup"
)

const (
	productionBaseURL  = "https://api.letsdeel.com/rest"
	sandboxBaseURL     = "https://api-sandbox.demo.deel.com/rest"
	stableAPIVersion   = "2026-01-01"
	requestTimeout     = 60 * time.Second
	maxPageSize        = 100
	maxInvoicePageSize = 50
	maxPages           = 100000
	fanoutWorkers      = 4
	// Deel only returns payroll cycles fully contained in a query and caps ranges at one year.
	payrollWindowMonths    = 6
	payrollLookaheadMonths = 5

	// Deel allows 5 requests per second per organization. Four requests per second
	// with no burst leaves headroom for other clients sharing the organization quota.
	rateLimit      = 4
	rateLimitBurst = 1
)

type paginationStyle int

const (
	paginationNone paginationStyle = iota
	paginationOffset
	paginationPageCursor
	paginationNextCursor
	paginationCursor
	paginationDataNextCursor
	paginationTimeOffNext
	paginationOffboarding
)

type intervalFormat int

const (
	intervalNone intervalFormat = iota
	intervalDate
	intervalDateTime
)

type endpointMeta struct {
	path                string
	primaryKeys         []string
	incrementalKey      string
	strategy            config.IncrementalStrategy
	pagination          paginationStyle
	pageSize            int
	pageSizeParam       string
	offsetParam         string
	cursorParam         string
	dataPath            string
	primitiveField      string
	query               map[string]string
	queryValues         url.Values
	version             string
	intervalStartParam  string
	intervalEndParam    string
	intervalFormat      intervalFormat
	startParamExclusive bool
	clientFilter        bool
}

type fanoutMeta struct {
	parentTable       string
	parentIDField     string
	parentOutputField string
	pathPlaceholder   string
	endpoint          endpointMeta
	allowedTypes      map[string]bool
	skipNotFound      bool
	defaultFiveYears  bool
}

var directTables = map[string]endpointMeta{
	"organizations": {
		path: "/organizations", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"legal_entities": {
		path: "/legal-entities", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationPageCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		query: map[string]string{"include_archived": "true", "include_payroll_settings": "true"}, clientFilter: true,
	},
	"people": {
		path: "/people", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
	},
	"contracts": {
		path: "/contracts", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationPageCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "after_cursor",
		query: map[string]string{"expand": "cost_centers"},
	},
	"contract_templates": {
		path: "/contract-templates", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"contract_termination_reasons": {
		path: "/contracts/termination-reasons", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"contract_custom_fields": {
		path: "/contracts/custom_fields", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"people_custom_fields": {
		path: "/people/custom_fields", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"departments": {
		path: "/departments", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"teams": {
		path: "/teams", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"groups": {
		path: "/groups", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationPageCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		query: map[string]string{"include_archived_groups": "true"}, clientFilter: true,
	},
	"roles": {
		path: "/roles", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"working_locations": {
		path: "/working-locations", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"managers": {
		path: "/managers", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset",
	},
	"onboarding": {
		path: "/onboarding/tracker", primaryKeys: []string{"unique_id"}, strategy: config.StrategyReplace,
		pagination: paginationPageCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
	},
	"offboarding": {
		path: "/offboarding/tracker", primaryKeys: []string{"unique_id"}, strategy: config.StrategyReplace,
		pagination: paginationOffboarding, pageSize: maxPageSize, pageSizeParam: "limit",
		query: map[string]string{"ignore_date_range": "true"},
	},
	"organization_tasks": {
		path: "/organizations/tasks", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		queryValues: url.Values{"statuses": {"PENDING", "COMPLETED", "DISMISSED", "FAILED"}},
	},
	"organization_structures": {
		path: "/hris/organization_structures", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset",
	},
	"worker_relation_types": {
		path: "/hris/worker_relations/types", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"industries": {
		path: "/industries", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"timesheets": {
		path: "/timesheets", primaryKeys: []string{"id"}, incrementalKey: "date_submitted", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
		intervalStartParam: "date_from", intervalEndParam: "date_to", intervalFormat: intervalDate,
	},
	"invoice_adjustments": {
		path: "/invoice-adjustments", primaryKeys: []string{"id"}, incrementalKey: "date_submitted", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
		intervalStartParam: "date_from", intervalEndParam: "date_to", intervalFormat: intervalDate,
	},
	"invoices": {
		path: "/invoices", primaryKeys: []string{"id"}, incrementalKey: "issued_at", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxInvoicePageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
		query:              map[string]string{"status": "all"},
		intervalStartParam: "issued_from_date", intervalEndParam: "issued_to_date", intervalFormat: intervalDate,
	},
	"deel_invoices": {
		path: "/invoices/deel", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationOffset, pageSize: maxInvoicePageSize, pageSizeParam: "limit", offsetParam: "offset",
	},
	"payments": {
		path: "/payments", primaryKeys: []string{"id"}, incrementalKey: "created_at", strategy: config.StrategyMerge,
		pagination: paginationDataNextCursor, cursorParam: "cursor", dataPath: "data.rows", clientFilter: true,
		intervalStartParam: "date_from", intervalEndParam: "date_to", intervalFormat: intervalDate,
	},
	"refund_statements": {
		path: "/refund-statements", primaryKeys: []string{"id"}, incrementalKey: "created_at", strategy: config.StrategyMerge,
		pagination: paginationDataNextCursor, cursorParam: "cursor", dataPath: "data.rows", clientFilter: true,
		intervalStartParam: "date_from", intervalEndParam: "date_to", intervalFormat: intervalDate,
	},
	"time_offs": {
		path: "/time_offs", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationTimeOffNext, pageSize: maxPageSize, pageSizeParam: "page_size", cursorParam: "next",
		query:              map[string]string{"include_deleted_time_offs": "true"},
		intervalStartParam: "updated_start_date", intervalEndParam: "updated_end_date", intervalFormat: intervalDateTime,
	},
	"shift_rates": {
		path: "/v2/time_tracking/shift_rates", primaryKeys: []string{"external_id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
	},
	"shifts": {
		path: "/v2/time_tracking/shifts", primaryKeys: []string{"external_id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset", clientFilter: true,
	},
	"adjustment_categories": {
		path: "/adjustments/categories", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		queryValues: url.Values{"contract_types": {"peo", "global_payroll", "hris_direct_employee", "eor", "employee", "independent_contractor"}},
	},
	"hourly_report_root_presets": {
		path: "/timesheets/root-presets", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationPageCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
	},
	"countries": {
		path: "/lookups/countries", primaryKeys: []string{"code"}, strategy: config.StrategyReplace,
	},
	"currencies": {
		path: "/lookups/currencies", primaryKeys: []string{"code"}, strategy: config.StrategyReplace,
	},
	"job_titles": {
		path: "/lookups/job-titles", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationPageCursor, cursorParam: "after_cursor",
	},
	"seniorities": {
		path: "/lookups/seniorities", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		query: map[string]string{"is_eor_contract": "false"},
	},
	"time_off_types": {
		path: "/lookups/time-off-types", primaryKeys: []string{"name"}, strategy: config.StrategyReplace, primitiveField: "name",
	},
	"webhooks": {
		path: "/webhooks", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"webhook_event_types": {
		path: "/webhooks/events/types", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true,
	},
	"it_assets": {
		path: "/it/assets", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"it_orders": {
		path: "/it/orders", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"it_policies": {
		path: "/it/policies", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
	},
	"immigration_cases": {
		path: "/immigration/client/cases", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_application_sources": {
		path: "/ats/application-sources", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_applications": {
		path: "/ats/applications", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
	},
	"ats_candidates": {
		path: "/ats/candidates", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		intervalStartParam: "updated_after", intervalFormat: intervalDateTime, startParamExclusive: true, clientFilter: true,
	},
	"ats_departments": {
		path: "/ats/departments", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_employment_types": {
		path: "/ats/employment-types", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_hiring_members": {
		path: "/ats/hiring-members", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_job_boards": {
		path: "/ats/job-boards", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_jobs": {
		path: "/ats/jobs", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		intervalStartParam: "updated_after", intervalFormat: intervalDateTime, startParamExclusive: true, clientFilter: true,
	},
	"ats_locations": {
		path: "/ats/locations", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_offers": {
		path: "/ats/offers", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true,
		pagination: paginationNextCursor, cursorParam: "cursor",
	},
	"ats_candidate_archivation_reasons": {
		path: "/ats/reasons", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		query: map[string]string{"reason_group_slug": "CANDIDATE_ARCHIVATION"},
	},
	"ats_offer_rejection_reasons": {
		path: "/ats/reasons", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		query: map[string]string{"reason_group_slug": "OFFER_REJECTION"},
	},
	"ats_job_closure_reasons": {
		path: "/ats/reasons", primaryKeys: []string{"id"}, strategy: config.StrategyReplace,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		query: map[string]string{"reason_group_slug": "JOB_CLOSURE"},
	},
	"ats_email_templates": {
		path: "/ats/email-templates", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
		intervalStartParam: "updated_after", intervalFormat: intervalDateTime, startParamExclusive: true, clientFilter: true,
	},
	"ats_interviews": {
		path: "/ats/interviews", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", version: "2026-06-03",
		intervalStartParam: "updated_after", intervalFormat: intervalDateTime, startParamExclusive: true, clientFilter: true,
	},
	"ats_job_postings": {
		path: "/ats/job-postings", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_openings": {
		path: "/ats/openings", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", version: "2026-06-15", clientFilter: true,
	},
	"ats_tags": {
		path: "/ats/tags", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
	},
	"ats_teams": {
		path: "/ats/teams", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
		pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", version: "2026-06-24", clientFilter: true,
	},
}

var contractorTypes = map[string]bool{
	"ongoing_time_based":       true,
	"milestones":               true,
	"time_based":               true,
	"pay_as_you_go_time_based": true,
	"commission":               true,
	"payg_milestones":          true,
	"payg_tasks":               true,
}

var fanoutTables = map[string]fanoutMeta{
	"contract_details": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "id", pathPlaceholder: "{contract_id}",
		endpoint: endpointMeta{path: "/contracts/{contract_id}", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true},
	},
	"contract_adjustments": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}",
		endpoint: endpointMeta{path: "/contracts/{contract_id}/adjustments", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true},
	},
	"contract_amendments": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: contractorTypes,
		endpoint: endpointMeta{
			path: "/contracts/{contract_id}/amendments", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
			pagination: paginationCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
		},
	},
	"contract_custom_field_values": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/contracts/{contract_id}/custom_fields", primaryKeys: []string{"contract_id", "id"}, strategy: config.StrategyReplace},
	},
	"contract_ic_invoicing_taxes": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: contractorTypes,
		endpoint: endpointMeta{path: "/contracts/{contract_id}/ic-invoicing-taxes", primaryKeys: []string{"contract_id"}, strategy: config.StrategyReplace},
	},
	"contract_milestones": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}",
		allowedTypes: map[string]bool{"milestones": true, "payg_milestones": true},
		endpoint:     endpointMeta{path: "/contracts/{contract_id}/milestones", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	},
	"contract_off_cycle_payments": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: contractorTypes,
		endpoint: endpointMeta{path: "/contracts/{contract_id}/off-cycle-payments", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	},
	"contract_payment_cycles": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: contractorTypes, skipNotFound: true,
		endpoint: endpointMeta{path: "/contracts/{contract_id}/payment_cycles", primaryKeys: []string{"contract_id", "id"}, strategy: config.StrategyReplace},
	},
	"contract_tasks": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: contractorTypes,
		endpoint: endpointMeta{path: "/contracts/{contract_id}/tasks", primaryKeys: []string{"contract_id", "id"}, strategy: config.StrategyReplace},
	},
	"eor_benefits": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: map[string]bool{"eor": true}, skipNotFound: true,
		endpoint: endpointMeta{path: "/eor/{contract_id}/benefits", primaryKeys: []string{"contract_id", "id"}, strategy: config.StrategyReplace},
	},
	"eor_amendments": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: map[string]bool{"eor": true},
		endpoint: endpointMeta{path: "/eor/contracts/{contract_id}/amendments", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true},
	},
	"eor_documents": {
		parentTable: "contracts", parentIDField: "id", parentOutputField: "contract_id", pathPlaceholder: "{contract_id}", allowedTypes: map[string]bool{"eor": true},
		endpoint: endpointMeta{path: "/eor/contracts/{contract_id}/documents", primaryKeys: []string{"contract_id", "document_type"}, strategy: config.StrategyReplace},
	},
	"people_details": {
		parentTable: "people", parentIDField: "id", parentOutputField: "id", pathPlaceholder: "{person_id}", skipNotFound: true,
		endpoint: endpointMeta{
			path: "/people/{person_id}", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge,
			query: map[string]string{"include_custom_fields": "true", "include_worker_relations": "true"}, clientFilter: true,
		},
	},
	"person_custom_field_values": {
		parentTable: "people", parentIDField: "id", parentOutputField: "person_id", pathPlaceholder: "{person_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/people/{person_id}/custom_fields", primaryKeys: []string{"person_id", "id"}, strategy: config.StrategyReplace},
	},
	"people_personal": {
		parentTable: "people", parentIDField: "id", parentOutputField: "person_id", pathPlaceholder: "{person_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/people/{person_id}/personal", primaryKeys: []string{"person_id"}, strategy: config.StrategyReplace},
	},
	"people_positions": {
		parentTable: "people", parentIDField: "id", parentOutputField: "person_id", pathPlaceholder: "{person_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/hris/positions/profile/{person_id}", primaryKeys: []string{"person_id", "id"}, strategy: config.StrategyReplace},
	},
	"people_worker_relations": {
		parentTable: "people", parentIDField: "id", parentOutputField: "person_id", pathPlaceholder: "{person_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/hris/worker_relations/profile/{person_id}", primaryKeys: []string{"person_id", "id"}, strategy: config.StrategyReplace},
	},
	"legal_entity_cost_centers": {
		parentTable: "legal_entities", parentIDField: "id", parentOutputField: "legal_entity_id", pathPlaceholder: "{legal_entity_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/legal-entities/{legal_entity_id}/cost-centers", primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge, clientFilter: true},
	},
	"payroll_cycles": {
		parentTable: "legal_entities", parentIDField: "id", parentOutputField: "legal_entity_id", pathPlaceholder: "{legal_entity_id}", skipNotFound: true, defaultFiveYears: true,
		endpoint: endpointMeta{
			path: "/legal-entities/{legal_entity_id}/payroll-events", primaryKeys: []string{"id"}, incrementalKey: "date_start", strategy: config.StrategyMerge,
			pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor", clientFilter: true,
			intervalStartParam: "date_start", intervalEndParam: "date_end", intervalFormat: intervalDate,
		},
	},
	"gp_payroll_events": {
		parentTable: "legal_entities", parentIDField: "id", parentOutputField: "legal_entity_id", pathPlaceholder: "{legal_entity_id}", skipNotFound: true,
		endpoint: endpointMeta{path: "/gp/legal-entities/{legal_entity_id}/reports", primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	},
}

type credentials struct {
	apiKey      string
	environment string
}

type DeelSource struct {
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

func NewDeelSource() *DeelSource {
	return &DeelSource{}
}

func (s *DeelSource) Schemes() []string {
	return []string{"deel"}
}

func (s *DeelSource) HandlesIncrementality() bool {
	return true
}

func (s *DeelSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = creds.apiKey
	if s.baseURL == "" {
		s.baseURL = productionBaseURL
		if creds.environment == "sandbox" {
			s.baseURL = sandboxBaseURL
		}
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(s.baseURL),
		httpclient.WithTimeout(requestTimeout),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("X-Version", stableAPIVersion),
	)

	config.Debug("[DEEL] Connected successfully")
	return nil
}

func parseURI(uri string) (credentials, error) {
	if !strings.HasPrefix(uri, "deel://") {
		return credentials{}, fmt.Errorf("invalid deel URI: must start with deel://")
	}

	rest := strings.TrimPrefix(uri, "deel://")
	if rest == "" || rest == "?" {
		return credentials{}, fmt.Errorf("api_key is required in deel URI")
	}

	values, err := url.ParseQuery(strings.TrimPrefix(rest, "?"))
	if err != nil {
		return credentials{}, fmt.Errorf("failed to parse deel URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return credentials{}, fmt.Errorf("api_key is required in deel URI")
	}

	environment := values.Get("environment")
	if environment == "" {
		environment = "production"
	}
	if environment != "production" && environment != "sandbox" {
		return credentials{}, fmt.Errorf("invalid deel environment %q: must be production or sandbox", environment)
	}

	return credentials{apiKey: apiKey, environment: environment}, nil
}

func (s *DeelSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *DeelSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	meta, ok := tableMeta(req.Name)
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTableNames(), ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("deel source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func tableMeta(table string) (endpointMeta, bool) {
	if meta, ok := directTables[table]; ok {
		return meta, true
	}
	if meta, ok := fanoutTables[table]; ok {
		return meta.endpoint, true
	}
	switch table {
	case "gross_to_net_reports":
		return endpointMeta{primaryKeys: []string{"payroll_cycle_id", "contract_oid"}, strategy: config.StrategyMerge}, true
	case "gp_gross_to_net_reports":
		return endpointMeta{primaryKeys: []string{"payroll_event_id", "contractId"}, strategy: config.StrategyMerge}, true
	default:
		return endpointMeta{}, false
	}
}

func supportedTableNames() []string {
	tables := make([]string, 0, len(directTables)+len(fanoutTables)+2)
	for table := range directTables {
		tables = append(tables, table)
	}
	for table := range fanoutTables {
		tables = append(tables, table)
	}
	tables = append(tables, "gross_to_net_reports", "gp_gross_to_net_reports")
	sort.Strings(tables)
	return tables
}

func isValidTable(table string) bool {
	_, ok := tableMeta(table)
	return ok
}

func (s *DeelSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		var err error
		switch table {
		case "gross_to_net_reports":
			err = s.readGrossToNetReports(ctx, opts, results)
		case "gp_gross_to_net_reports":
			err = s.readGPGrossToNetReports(ctx, opts, results)
		default:
			if meta, ok := directTables[table]; ok {
				err = s.readDirect(ctx, table, meta, opts, results)
			} else {
				err = s.readFanout(ctx, table, fanoutTables[table], opts, results)
			}
		}

		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
		}
	}()

	return results, nil
}

func (s *DeelSource) readDirect(ctx context.Context, table string, meta endpointMeta, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[DEEL] reading %s", table)
	return s.fetchPages(ctx, meta, opts, func(items []map[string]any) error {
		return sendItems(ctx, items, meta, opts, results)
	})
}

func (s *DeelSource) readFanout(ctx context.Context, table string, meta fanoutMeta, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[DEEL] reading %s", table)
	childOptions := []source.ReadOptions{opts}
	if meta.defaultFiveYears {
		childOptions = payrollIntervals(opts)
	}

	parentMeta := directTables[meta.parentTable]
	return s.fetchPages(ctx, parentMeta, source.ReadOptions{}, func(parents []map[string]any) error {
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(fanoutWorkers)
		for _, parent := range parents {
			parent := parent
			group.Go(func() error {
				if len(meta.allowedTypes) > 0 && !meta.allowedTypes[stringValue(parent["type"])] {
					return nil
				}

				parentID := stringValue(parent[meta.parentIDField])
				if parentID == "" {
					return nil
				}

				childMeta := meta.endpoint
				childMeta.path = strings.ReplaceAll(childMeta.path, meta.pathPlaceholder, url.PathEscape(parentID))
				for _, childOpts := range childOptions {
					requestOpts := childOpts
					if meta.defaultFiveYears {
						requestOpts = payrollRequestOptions(childOpts)
					}
					err := s.fetchPages(groupCtx, childMeta, requestOpts, func(items []map[string]any) error {
						for _, item := range items {
							item[meta.parentOutputField] = parentID
						}
						return sendItems(groupCtx, items, childMeta, childOpts, results)
					})
					if err != nil {
						var statusErr *apiStatusError
						if meta.skipNotFound && errors.As(err, &statusErr) && statusErr.statusCode == http.StatusNotFound {
							return nil
						}
						return fmt.Errorf("failed to fetch %s for parent: %w", table, err)
					}
				}
				return nil
			})
		}
		return group.Wait()
	})
}

func (s *DeelSource) readGrossToNetReports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[DEEL] reading gross_to_net_reports")
	cycleOptions := payrollIntervals(opts)
	entityMeta := directTables["legal_entities"]
	return s.fetchPages(ctx, entityMeta, source.ReadOptions{}, func(entities []map[string]any) error {
		for _, entity := range entities {
			entityID := stringValue(entity["id"])
			if entityID == "" {
				continue
			}

			cycleMeta := fanoutTables["payroll_cycles"].endpoint
			cycleMeta.path = strings.ReplaceAll(cycleMeta.path, "{legal_entity_id}", url.PathEscape(entityID))
			for _, cycleOpts := range cycleOptions {
				err := s.fetchPages(ctx, cycleMeta, payrollRequestOptions(cycleOpts), func(cycles []map[string]any) error {
					cycles = filterItemsByInterval(cycles, cycleMeta.incrementalKey, cycleOpts.IntervalStart, cycleOpts.IntervalEnd)
					for _, cycle := range cycles {
						if hasReport, ok := cycle["has_g2n_report"].(bool); ok && !hasReport {
							continue
						}
						cycleID := stringValue(cycle["id"])
						if cycleID == "" {
							continue
						}

						reportMeta := endpointMeta{
							path:       "/reports/payroll/cycles/" + url.PathEscape(cycleID) + "/gross-to-net",
							pagination: paginationNextCursor, pageSize: maxPageSize, pageSizeParam: "limit", cursorParam: "cursor",
						}
						if err := s.fetchPages(ctx, reportMeta, source.ReadOptions{}, func(items []map[string]any) error {
							for _, item := range items {
								item["payroll_cycle_id"] = cycleID
								item["legal_entity_id"] = entityID
							}
							return sendItems(ctx, items, reportMeta, source.ReadOptions{}, results)
						}); err != nil {
							return fmt.Errorf("failed to fetch gross-to-net report: %w", err)
						}
					}
					return nil
				})
				if err != nil {
					var statusErr *apiStatusError
					if errors.As(err, &statusErr) && statusErr.statusCode == http.StatusNotFound {
						break
					}
					return fmt.Errorf("failed to fetch payroll cycles: %w", err)
				}
			}
		}
		return nil
	})
}

func (s *DeelSource) readGPGrossToNetReports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[DEEL] reading gp_gross_to_net_reports")
	entityMeta := directTables["legal_entities"]
	return s.fetchPages(ctx, entityMeta, source.ReadOptions{}, func(entities []map[string]any) error {
		for _, entity := range entities {
			entityID := stringValue(entity["id"])
			if entityID == "" {
				continue
			}

			eventsMeta := fanoutTables["gp_payroll_events"].endpoint
			eventsMeta.path = strings.ReplaceAll(eventsMeta.path, "{legal_entity_id}", url.PathEscape(entityID))
			err := s.fetchPages(ctx, eventsMeta, source.ReadOptions{}, func(events []map[string]any) error {
				for _, event := range events {
					eventID := stringValue(event["id"])
					if eventID == "" {
						continue
					}

					reportMeta := endpointMeta{
						path:       "/gp/reports/" + url.PathEscape(eventID) + "/gross_to_net",
						pagination: paginationOffset, pageSize: maxPageSize, pageSizeParam: "limit", offsetParam: "offset",
					}
					if err := s.fetchPages(ctx, reportMeta, opts, func(items []map[string]any) error {
						flattenReportCell(items, "contractId")
						for _, item := range items {
							item["payroll_event_id"] = eventID
							item["legal_entity_id"] = entityID
						}
						return sendItems(ctx, items, reportMeta, opts, results)
					}); err != nil {
						return fmt.Errorf("failed to fetch GP gross-to-net report: %w", err)
					}
				}
				return nil
			})
			if err != nil {
				var statusErr *apiStatusError
				if errors.As(err, &statusErr) && statusErr.statusCode == http.StatusNotFound {
					continue
				}
				return fmt.Errorf("failed to fetch GP payroll events: %w", err)
			}
		}
		return nil
	})
}

type apiStatusError struct {
	path       string
	statusCode int
	body       string
}

func (e *apiStatusError) Error() string {
	return fmt.Sprintf("Deel API %s returned status %d: %s", e.path, e.statusCode, e.body)
}

func (s *DeelSource) fetchPages(ctx context.Context, meta endpointMeta, opts source.ReadOptions, onPage func([]map[string]any) error) error {
	cursor := ""
	cursorParams := map[string]string{}
	offset := 0
	dataPath := meta.dataPath
	if dataPath == "" {
		dataPath = "data"
	}

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		if len(meta.query) > 0 {
			req.SetQueryParams(meta.query)
		}
		if len(meta.queryValues) > 0 {
			req.SetQueryParamValues(meta.queryValues)
		}
		if meta.version != "" {
			req.SetHeader("X-Version", meta.version)
		}
		if meta.pageSizeParam != "" && meta.pageSize > 0 {
			req.SetQueryParam(meta.pageSizeParam, strconv.Itoa(meta.pageSize))
		}
		if meta.pagination == paginationOffset {
			req.SetQueryParam(meta.offsetParam, strconv.Itoa(offset))
		} else if meta.pagination == paginationOffboarding {
			for key, value := range cursorParams {
				req.SetQueryParam("pagination["+key+"]", value)
			}
		} else if cursor != "" {
			req.SetQueryParam(meta.cursorParam, cursor)
		}
		setIntervalParams(req, meta, opts)

		resp, err := req.Get(meta.path)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", meta.path, err)
		}
		if !resp.IsSuccess() {
			return &apiStatusError{path: meta.path, statusCode: resp.StatusCode(), body: resp.String()}
		}

		body, err := decodeObject(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", meta.path, err)
		}
		items, err := extractItems(body, dataPath, meta.primitiveField)
		if err != nil {
			return fmt.Errorf("failed to parse %s items: %w", meta.path, err)
		}

		if len(items) > 0 {
			if err := onPage(items); err != nil {
				return err
			}
		}
		config.Debug("[DEEL] fetched %d items from %s", len(items), meta.path)

		switch meta.pagination {
		case paginationNone:
			return nil
		case paginationOffset:
			if len(items) < meta.pageSize {
				return nil
			}
			offset += meta.pageSize
		case paginationPageCursor:
			cursor = nestedString(body, "page", "cursor")
			if cursor == "" {
				return nil
			}
		case paginationNextCursor:
			cursor = nestedString(body, "next_cursor")
			if cursor == "" {
				return nil
			}
		case paginationCursor:
			cursor = nestedString(body, "cursor")
			if cursor == "" {
				return nil
			}
		case paginationDataNextCursor:
			cursor = nestedString(body, "data", "next_cursor")
			if cursor == "" {
				return nil
			}
		case paginationTimeOffNext:
			cursor = nestedString(body, "next")
			if cursor == "" {
				return nil
			}
		case paginationOffboarding:
			cursor = nestedString(body, "page", "cursor")
			if cursor == "" {
				return nil
			}
			cursorParams, err = decodeOffboardingCursor(cursor)
			if err != nil {
				return fmt.Errorf("failed to decode offboarding cursor: %w", err)
			}
		}
	}

	config.Debug("[DEEL] stopped %s after reaching maxPages=%d", meta.path, maxPages)
	return fmt.Errorf("stopped fetching %s after %d pages", meta.path, maxPages)
}

func setIntervalParams(req *httpclient.Request, meta endpointMeta, opts source.ReadOptions) {
	if meta.intervalFormat == intervalNone {
		return
	}

	format := time.RFC3339Nano
	if meta.intervalFormat == intervalDate {
		format = time.DateOnly
	}
	if opts.IntervalStart != nil && meta.intervalStartParam != "" {
		start := opts.IntervalStart.UTC()
		if meta.startParamExclusive {
			start = start.Add(-time.Second)
		}
		req.SetQueryParam(meta.intervalStartParam, start.Format(format))
	}
	if opts.IntervalEnd != nil && meta.intervalEndParam != "" {
		end := opts.IntervalEnd.UTC()
		if meta.intervalFormat == intervalDate && end != end.Truncate(24*time.Hour) {
			end = end.Truncate(24*time.Hour).AddDate(0, 0, 1)
		}
		req.SetQueryParam(meta.intervalEndParam, end.Format(format))
	}
}

func decodeObject(data []byte) (map[string]any, error) {
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func extractItems(body map[string]any, dataPath, primitiveField string) ([]map[string]any, error) {
	var value any = body
	for _, part := range strings.Split(dataPath, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s is not an object", strings.Join(strings.Split(dataPath, "."), "."))
		}
		value = object[part]
	}

	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return []map[string]any{typed}, nil
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, raw := range typed {
			if item, ok := raw.(map[string]any); ok {
				items = append(items, item)
				continue
			}
			if primitiveField == "" {
				return nil, fmt.Errorf("received a primitive item without a field mapping")
			}
			items = append(items, map[string]any{primitiveField: raw})
		}
		return items, nil
	default:
		if primitiveField != "" {
			return []map[string]any{{primitiveField: typed}}, nil
		}
		return nil, fmt.Errorf("unexpected item container %T", value)
	}
}

func sendItems(ctx context.Context, items []map[string]any, meta endpointMeta, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if meta.clientFilter {
		items = filterItemsByInterval(items, meta.incrementalKey, opts.IntervalStart, opts.IntervalEnd)
	}
	if len(items) == 0 {
		return nil
	}

	flush := func(batch []map[string]any) error {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert Deel records to Arrow: %w", err)
		}
		return sendResult(ctx, results, source.RecordBatchResult{Batch: record})
	}

	if opts.MaxBatchBytes <= 0 {
		return flush(items)
	}

	var batch []map[string]any
	var accBytes int64
	for _, item := range items {
		rowBytes := arrowconv.RowBytes(item)
		if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
			if err := flush(batch); err != nil {
				return err
			}
			batch = nil
			accBytes = 0
		}
		batch = append(batch, item)
		accBytes += rowBytes
	}
	if len(batch) > 0 {
		return flush(batch)
	}
	return nil
}

func sendResult(ctx context.Context, results chan<- source.RecordBatchResult, result source.RecordBatchResult) error {
	select {
	case results <- result:
		return nil
	case <-ctx.Done():
		if result.Batch != nil {
			result.Batch.Release()
		}
		return ctx.Err()
	}
}

func flattenReportCell(items []map[string]any, field string) {
	for _, item := range items {
		cell, ok := item[field].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := cell["currentValue"]; ok {
			item[field] = value
		}
	}
}

func filterItemsByInterval(items []map[string]any, field string, start, end *time.Time) []map[string]any {
	if field == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		timestamp, ok := parseTimestamp(item[field])
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && timestamp.Before(*start) {
			continue
		}
		if end != nil && !timestamp.Before(*end) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseTimestamp(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func nestedString(body map[string]any, path ...string) string {
	var value any = body
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[part]
	}
	return stringValue(value)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func decodeOffboardingCursor(cursor string) (map[string]string, error) {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 cursor: %w", err)
	}

	decoded, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor payload: %w", err)
	}
	pagination, ok := decoded["pagination"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cursor payload does not contain pagination fields")
	}

	params := make(map[string]string, len(pagination))
	for key, value := range pagination {
		params[key] = stringValue(value)
	}
	return params, nil
}

func withDefaultFiveYearInterval(opts source.ReadOptions) source.ReadOptions {
	now := time.Now().UTC()
	if opts.IntervalStart == nil {
		start := now.AddDate(-5, 0, 0)
		opts.IntervalStart = &start
	}
	if opts.IntervalEnd == nil {
		end := now
		opts.IntervalEnd = &end
	}
	return opts
}

func payrollIntervals(opts source.ReadOptions) []source.ReadOptions {
	opts = withDefaultFiveYearInterval(opts)
	start := opts.IntervalStart.UTC()
	end := opts.IntervalEnd.UTC()
	if !start.Before(end) {
		return nil
	}

	intervals := make([]source.ReadOptions, 0, 5)
	for start.Before(end) {
		windowEnd := start.AddDate(0, payrollWindowMonths, 0)
		if windowEnd.After(end) {
			windowEnd = end
		}
		windowStart := start
		windowOpts := opts
		windowOpts.IntervalStart = &windowStart
		windowOpts.IntervalEnd = &windowEnd
		intervals = append(intervals, windowOpts)
		start = windowEnd
	}
	return intervals
}

func payrollRequestOptions(opts source.ReadOptions) source.ReadOptions {
	if opts.IntervalEnd != nil {
		end := opts.IntervalEnd.AddDate(0, payrollLookaheadMonths, 0)
		opts.IntervalEnd = &end
	}
	return opts
}
