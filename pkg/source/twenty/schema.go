package twenty

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// ── Twenty's metadata API ────────────────────────────────────────────────────
//
//	GET /rest/metadata/objects
//	{"data":{"objects":[ {nameSingular, namePlural, isActive, fields:[…]} ]},
//	 "pageInfo":{"endCursor":…,"hasNextPage":false},"totalCount":25}
//
// This is what makes one generic reader cover every object, exactly like the
// sibling `abra` source: Twenty is metadata-driven, so the SAME code serves a
// stock workspace and a heavily customised one. It also has to — two workspaces
// of the same product genuinely differ: measured on two live workspaces, one had
// a `lead` object the other lacked, and their `person` objects carried 79 fields
// against 32. A hand-written
// table list would be wrong for one of them on day one.

// relationSettings is the `settings` blob. Twenty overloads this key per field
// type — relations put relationType/joinColumnName in it, NUMBER puts dataType.
// Decoding both into one struct is safe because the absent keys stay zero.
type relationSettings struct {
	RelationType   string `json:"relationType"`
	JoinColumnName string `json:"joinColumnName"`
	DataType       string `json:"dataType"`
}

type fieldMeta struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	IsActive bool              `json:"isActive"`
	Settings *relationSettings `json:"settings"`
}

type objectMeta struct {
	NameSingular string      `json:"nameSingular"`
	NamePlural   string      `json:"namePlural"`
	IsActive     bool        `json:"isActive"`
	Fields       []fieldMeta `json:"fields"`
}

// metadataCache memoises the object list for the lifetime of one ingestr run.
// GetTable is called once per table, and a multi-table job would otherwise
// re-read the whole metadata document per table — wasteful against a 100 req/min
// budget that the actual row reads need.
type metadataCache struct {
	once    sync.Once
	objects []objectMeta
	err     error
}

// fetchObjects reads every object metadata definition, following pagination.
//
// The live workspaces answer in a single page (25 objects), but the endpoint is
// cursor-paginated like every other Twenty collection and a workspace that grows
// past the default page would otherwise lose objects silently — the failure would
// surface as "unknown object" for a table that plainly exists in the UI.
func (s *Source) fetchObjects(ctx context.Context) ([]objectMeta, error) {
	s.meta.once.Do(func() {
		var all []objectMeta
		cursor := ""
		for page := 0; page < maxMetadataPages; page++ {
			req := s.client.R(ctx).SetQueryParam("limit", strconv.Itoa(metadataPageSize))
			if cursor != "" {
				req = req.SetQueryParam("starting_after", cursor)
			}
			resp, err := req.Get("/metadata/objects")
			if err != nil {
				s.meta.err = fmt.Errorf("twenty: failed to fetch object metadata: %w", err)
				return
			}
			if !resp.IsSuccess() {
				s.meta.err = fmt.Errorf("twenty: object metadata request returned HTTP %d: %s",
					resp.StatusCode(), truncate(resp.String(), 300))
				return
			}
			objs, info, err := decodeObjects([]byte(resp.String()))
			if err != nil {
				s.meta.err = err
				return
			}
			all = append(all, objs...)
			if !info.HasNextPage || info.EndCursor == "" || len(objs) == 0 {
				break
			}
			cursor = info.EndCursor
		}
		if len(all) == 0 {
			s.meta.err = fmt.Errorf("twenty: the workspace reported no objects at all — " +
				"this is an auth or endpoint problem, not an empty workspace")
			return
		}
		s.meta.objects = all
	})
	return s.meta.objects, s.meta.err
}

// decodeObjects unwraps {"data":{"objects":[…]},"pageInfo":{…}}.
func decodeObjects(body []byte) ([]objectMeta, pageInfo, error) {
	var env struct {
		Data struct {
			Objects []objectMeta `json:"objects"`
		} `json:"data"`
		PageInfo pageInfo `json:"pageInfo"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, pageInfo{}, fmt.Errorf("twenty: failed to parse object metadata: %w", err)
	}
	return env.Data.Objects, env.PageInfo, nil
}

// ── Column planning ──────────────────────────────────────────────────────────

// tablePlan is everything the reader needs about one object, derived once from
// the metadata so neither the declared schema nor the row projection can drift
// mid-run.
type tablePlan struct {
	object   string // namePlural — both the URL path and the response envelope key
	singular string
	columns  []schema.Column
	typeOf   map[string]schema.DataType
	// dropped are source keys we deliberately do not carry. Tracked so the drift
	// warning stays meaningful instead of firing on every row for searchVector.
	dropped      map[string]struct{}
	hasUpdatedAt bool
	hasDeletedAt bool
}

// sanitizeColumn keeps a Twenty field name usable as a column name.
//
// Twenty enforces camelCase API names, so in practice this is a no-op — it exists
// so a future custom field with an unexpected character fails as a renamed column
// rather than as unquoted-identifier SQL. Same minimal substitution as `abra`:
// every character outside [A-Za-z0-9_] becomes '_', casing preserved.
func sanitizeColumn(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// dataTypeFor maps a Twenty field type onto an ingestr/Arrow type.
//
// ⚠️ COMPOSITE FIELDS BECOME STRINGS CARRYING JSON TEXT, NOT schema.TypeJSON.
// Twenty's composites (EMAILS, PHONES, LINKS, ADDRESS, CURRENCY, FULL_NAME,
// ACTOR, RICH_TEXT_V2, ARRAY, MULTI_SELECT, RAW_JSON) are small JSON objects.
// TypeJSON is not handled uniformly across destinations — some have
// no mapping for it — so declaring it would land us on an untested path. String
// is what the sibling `abra` source does with the same class of value, and a
// reads it back with JSONExtractString, a pattern already in use here.
//
// ⚠️ CURRENCY IS THEREFORE NOT A DECIMAL, AND THAT IS DELIBERATE. Twenty carries
// money as {amountMicros, currencyCode} — integer micros, never a float — so the
// exact value survives as text and a downstream model divides by 1e6 explicitly.
// That also keeps this source clear of destination decimal handling for tables with
// a decimal column on its SECOND sync.
//
// NUMBER honours settings.dataType: ints stay ints; anything else keeps its exact
// decimal text rather than being rounded through a float.
func dataTypeFor(f fieldMeta) (schema.DataType, bool) {
	switch strings.ToUpper(strings.TrimSpace(f.Type)) {
	case "TS_VECTOR":
		// searchVector is Postgres' full-text index, echoed by REST as
		// "'notion':1 'notion.com':2". Bulky, derived, and of no analytical use.
		return 0, false
	case "BOOLEAN":
		return schema.TypeBoolean, true
	case "DATE_TIME":
		return schema.TypeTimestamp, true
	case "DATE":
		return schema.TypeDate, true
	case "POSITION":
		// Twenty's manual-ordering key. Fractional by design — it bisects to
		// insert between two rows.
		return schema.TypeFloat64, true
	case "NUMBER":
		if f.Settings != nil && !strings.EqualFold(f.Settings.DataType, "int") && f.Settings.DataType != "" {
			return schema.TypeString, true
		}
		return schema.TypeInt64, true
	default:
		// UUID, TEXT, RICH_TEXT, SELECT, RATING and every composite above.
		return schema.TypeString, true
	}
}

// buildPlan turns one object's metadata into a column plan.
//
// ⚠️ RELATIONS ARE THE WHOLE REASON THIS READS METADATA RATHER THAN GUESSING.
// At depth=0 Twenty returns the FOREIGN KEY, not the related object — a person
// carries `companyId`, never a nested `company`. But the metadata field is named
// `company`, and the FK name lives in settings.joinColumnName. So:
//
//	MANY_TO_ONE  -> declare settings.joinColumnName (companyId, ownerId, …)
//	ONE_TO_MANY  -> declare NOTHING; the child side owns that key
//
// Inferring the schema from a response instead would have worked, but a column
// that is entirely NULL in the first batch infers as the wrong type and a
// customised workspace has plenty of those. The join keys are also the single
// most valuable thing in a CRM extract, so guessing at them is the wrong trade.
func buildPlan(obj objectMeta) (*tablePlan, error) {
	p := &tablePlan{
		object:   obj.NamePlural,
		singular: obj.NameSingular,
		typeOf:   map[string]schema.DataType{},
		dropped:  map[string]struct{}{},
	}

	seen := map[string]struct{}{}
	add := func(sourceKey string, dt schema.DataType, isPK bool) {
		dest := sanitizeColumn(sourceKey)
		if dest == "" {
			return
		}
		if _, dup := seen[dest]; dup {
			return
		}
		seen[dest] = struct{}{}
		p.typeOf[dest] = dt
		p.columns = append(p.columns, schema.Column{
			Name:         dest,
			DataType:     dt,
			Nullable:     !isPK,
			IsPrimaryKey: isPK,
		})
	}

	hasID := false
	for _, f := range obj.Fields {
		if f.Name == "" || !f.IsActive {
			continue
		}
		if strings.EqualFold(f.Type, "RELATION") {
			if f.Settings != nil && strings.EqualFold(f.Settings.RelationType, "MANY_TO_ONE") &&
				f.Settings.JoinColumnName != "" {
				add(f.Settings.JoinColumnName, schema.TypeString, false)
				continue
			}
			// ONE_TO_MANY (and any relation without a join column): no column on
			// this side of the edge. Recorded so it is not reported as drift.
			p.dropped[f.Name] = struct{}{}
			continue
		}

		dt, keep := dataTypeFor(f)
		if !keep {
			p.dropped[f.Name] = struct{}{}
			continue
		}
		switch f.Name {
		case "id":
			hasID = true
			// Twenty ids are UUIDs and are never null.
			add("id", schema.TypeString, true)
			continue
		case updatedAtField:
			p.hasUpdatedAt = true
		case deletedAtField:
			p.hasDeletedAt = true
		}
		add(f.Name, dt, false)
	}

	// ⚠️ FAIL CLOSED ON A MISSING PRIMARY KEY. Without `id` there is nothing to
	// deduplicate on, and under an append-style incremental strategy every
	// run would append the full object forever. `count() FINAL` could not see it.
	if !hasID {
		return nil, fmt.Errorf("twenty: object %q exposes no `id` field and cannot be ingested safely", obj.NamePlural)
	}

	// ⚠️ DO NOT ADD A LOAD-TIMESTAMP COLUMN. ingestr's strategy layer sets
	// `_ingestr_loaded_at` itself, and the promote script uses exactly that column
	// as the ReplicatedReplacingMergeTree version.

	return p, nil
}

// sortedKeys returns map keys in a stable order for log lines.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
