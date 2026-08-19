# Deel

[Deel](https://www.deel.com/) is a global workforce platform for contracts, HR, payroll, expenses, time off, recruiting, immigration, and IT operations.

ingestr supports Deel as a source through the [Deel REST API](https://developer.deel.com/api/quickstart).

## URI format

```plaintext
deel://?api_key=<api_key>
deel://?api_key=<api_key>&environment=sandbox
```

URI parameters:

- `api_key`: A Deel organization API token. Give the token the read scopes needed by the tables you plan to ingest.
- `environment`: Optional. Use `production` (the default) or `sandbox`. Sandbox tokens are separate from production tokens.

Some tables contain sensitive personal, employment, and payroll information. Deel returns only the fields and resources allowed by the token's scopes and sensitive-data settings.

## Example usage

```bash
ingestr ingest \
  --source-uri 'deel://?api_key=<api_key>' \
  --source-table people \
  --dest-uri duckdb:///deel.duckdb \
  --dest-table main.people
```

Nested Deel objects and arrays are preserved as JSON columns. Tables whose names start with `contract_`, `people_`, `person_`, `eor_`, `legal_entity_`, `payroll_`, or `gp_` may make one request per matching parent record.

## Organization and workforce tables

| Table | Primary key | Strategy | Data |
| --- | --- | --- | --- |
| `organizations` | `id` | replace | Current organization |
| `legal_entities` | `id` | merge | Legal entities, including archived entities and payroll settings |
| `people` | `id` | merge | Organization-wide people directory and employment summaries |
| `people_details` | `id` | merge | Full profile for every person, including custom fields and worker relations |
| `people_personal` | `person_id` | replace | Personal information for every accessible person |
| `people_positions` | `person_id`, `id` | replace | HRIS positions for every person |
| `people_worker_relations` | `person_id`, `id` | replace | Worker relationships for every person |
| `person_custom_field_values` | `person_id`, `id` | replace | Custom-field values for every person |
| `people_custom_fields` | `id` | replace | People custom-field definitions |
| `departments` | `id` | replace | Departments |
| `teams` | `id` | replace | Teams |
| `groups` | `id` | merge | Groups, including archived groups |
| `roles` | `id` | replace | Organization roles |
| `working_locations` | `id` | replace | Working locations |
| `managers` | `id` | replace | Managers |
| `onboarding` | `unique_id` | replace | Onboarding tracker records |
| `offboarding` | `unique_id` | replace | Offboarding tracker records |
| `organization_tasks` | `id` | replace | Organization tasks |
| `organization_structures` | `id` | replace | HRIS organization structures |
| `worker_relation_types` | `id` | replace | Worker-relation type definitions |
| `industries` | `id` | merge | Industry subcategories |

## Contract, time, payroll, and accounting tables

| Table | Primary key | Strategy | Data |
| --- | --- | --- | --- |
| `contracts` | `id` | replace | Contract summaries and cost centers |
| `contract_details` | `id` | merge | Full details for every contract |
| `contract_templates` | `id` | replace | Contract templates |
| `contract_termination_reasons` | `id` | merge | Contract termination reasons |
| `contract_custom_fields` | `id` | replace | Contract custom-field definitions |
| `contract_custom_field_values` | `contract_id`, `id` | replace | Custom-field values for every contract |
| `contract_adjustments` | `id` | merge | Adjustments for every contract |
| `contract_amendments` | `id` | merge | Amendments for every contract |
| `contract_ic_invoicing_taxes` | `contract_id` | replace | Independent-contractor invoicing taxes |
| `contract_milestones` | `id` | replace | Milestones for milestone contracts |
| `contract_off_cycle_payments` | `id` | replace | Contractor off-cycle payments |
| `contract_payment_cycles` | `contract_id`, `id` | replace | Contractor payment cycles |
| `contract_tasks` | `contract_id`, `id` | replace | Contractor tasks |
| `eor_benefits` | `contract_id`, `id` | replace | Benefits for EOR contracts |
| `eor_amendments` | `id` | merge | Amendments for EOR contracts |
| `eor_documents` | `contract_id`, `document_type` | replace | Documents for EOR contracts |
| `timesheets` | `id` | merge | Timesheets |
| `hourly_report_root_presets` | `id` | replace | Hourly-report root presets |
| `adjustment_categories` | `id` | replace | Adjustment categories |
| `invoice_adjustments` | `id` | merge | Invoice adjustments |
| `invoices` | `id` | merge | Organization invoices |
| `deel_invoices` | `id` | replace | Deel fee invoices |
| `payments` | `id` | merge | Payment receipts |
| `refund_statements` | `id` | merge | Refund statements |
| `time_offs` | `id` | merge | Time-off requests, including deleted requests |
| `shift_rates` | `external_id` | merge | Time-tracking shift rates |
| `shifts` | `external_id` | merge | Time-tracking shifts |
| `legal_entity_cost_centers` | `id` | merge | Cost centers for every legal entity |
| `payroll_cycles` | `id` | merge | Payroll cycles for every legal entity |
| `gross_to_net_reports` | `payroll_cycle_id`, `contract_oid` | merge | Gross-to-net records for payroll cycles |
| `gp_payroll_events` | `id` | replace | Global Payroll events for every legal entity |
| `gp_gross_to_net_reports` | `payroll_event_id`, `contractId` | merge | Global Payroll gross-to-net records |

## Recruiting, IT, immigration, and webhook tables

| Table | Primary key | Strategy | Data |
| --- | --- | --- | --- |
| `ats_application_sources` | `id` | merge | ATS application sources |
| `ats_applications` | `id` | replace | ATS applications |
| `ats_candidates` | `id` | merge | ATS candidates |
| `ats_departments` | `id` | merge | ATS departments |
| `ats_employment_types` | `id` | merge | ATS employment types |
| `ats_hiring_members` | `id` | merge | ATS hiring members |
| `ats_job_boards` | `id` | merge | ATS job boards |
| `ats_jobs` | `id` | merge | ATS jobs |
| `ats_locations` | `id` | merge | ATS locations |
| `ats_offers` | `id` | merge | ATS offers |
| `ats_candidate_archivation_reasons` | `id` | replace | Candidate archivation reasons |
| `ats_offer_rejection_reasons` | `id` | replace | Offer rejection reasons |
| `ats_job_closure_reasons` | `id` | replace | Job closure reasons |
| `ats_email_templates` | `id` | merge | Published ATS email templates |
| `ats_interviews` | `id` | merge | ATS interviews |
| `ats_job_postings` | `id` | merge | ATS job postings |
| `ats_openings` | `id` | merge | ATS job openings |
| `ats_tags` | `id` | merge | ATS candidate tags |
| `ats_teams` | `id` | merge | ATS teams |
| `it_assets` | `id` | merge | Deel IT assets |
| `it_orders` | `id` | merge | Deel IT orders |
| `it_policies` | `id` | replace | Deel IT hardware policies |
| `immigration_cases` | `id` | merge | Client immigration cases |
| `webhooks` | `id` | replace | Configured webhooks |
| `webhook_event_types` | `id` | merge | Available webhook event types |

## Lookup tables

| Table | Primary key | Data |
| --- | --- | --- |
| `countries` | `code` | Supported countries and country capabilities |
| `currencies` | `code` | Supported currencies |
| `job_titles` | `id` | Job titles |
| `seniorities` | `id` | Seniority levels |
| `time_off_types` | `name` | Time-off type names |

Feature-specific tables return an API error when the corresponding Deel product or token scope is not enabled for the organization.
