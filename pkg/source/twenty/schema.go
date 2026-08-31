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

type metadataCache struct {
	once    sync.Once
	objects []objectMeta
	err     error
}

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

type tablePlan struct {
	object       string
	singular     string
	columns      []schema.Column
	typeOf       map[string]schema.DataType
	dropped      map[string]struct{}
	hasUpdatedAt bool
	hasDeletedAt bool
}

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

func dataTypeFor(f fieldMeta) (schema.DataType, bool) {
	switch strings.ToUpper(strings.TrimSpace(f.Type)) {
	case "TS_VECTOR":
		return 0, false
	case "BOOLEAN":
		return schema.TypeBoolean, true
	case "DATE_TIME":
		return schema.TypeTimestamp, true
	case "DATE":
		return schema.TypeDate, true
	case "POSITION":
		return schema.TypeFloat64, true
	case "NUMBER":
		if f.Settings != nil && !strings.EqualFold(f.Settings.DataType, "int") && f.Settings.DataType != "" {
			return schema.TypeString, true
		}
		return schema.TypeInt64, true
	case "EMAILS", "PHONES", "LINKS", "ADDRESS", "CURRENCY", "FULL_NAME", "ACTOR", "RICH_TEXT_V2", "ARRAY", "MULTI_SELECT", "RAW_JSON":
		return schema.TypeJSON, true
	default:
		return schema.TypeString, true
	}
}

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
			add("id", schema.TypeString, true)
			continue
		case updatedAtField:
			p.hasUpdatedAt = true
		case deletedAtField:
			p.hasDeletedAt = true
		}
		add(f.Name, dt, false)
	}

	if !hasID {
		return nil, fmt.Errorf("twenty: object %q exposes no `id` field and cannot be ingested safely", obj.NamePlural)
	}

	return p, nil
}

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
