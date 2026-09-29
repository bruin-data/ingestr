package attio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
)

// attribute is one attribute from GET /v2/objects/{object}/attributes.
type attribute struct {
	APISlug       string `json:"api_slug"`
	Title         string `json:"title"`
	Type          string `json:"type"`
	IsWritable    bool   `json:"is_writable"`
	IsUnique      bool   `json:"is_unique"`
	IsMultiselect bool   `json:"is_multiselect"`
	IsArchived    bool   `json:"is_archived"`
	Config        struct {
		RecordReference struct {
			AllowedObjectIDs []string `json:"allowed_object_ids"`
		} `json:"record_reference"`
	} `json:"config"`
}

type objectInfo struct {
	ID struct {
		ObjectID string `json:"object_id"`
	} `json:"id"`
	APISlug      string `json:"api_slug"`
	SingularNoun string `json:"singular_noun"`
	PluralNoun   string `json:"plural_noun"`
}

// objectMeta is an object's attributes, indexed by lowercased api_slug.
type objectMeta struct {
	slug   string
	bySlug map[string]attribute
	// refTargets maps a lowercased record-reference slug to the api_slugs of the
	// objects it may point to.
	refTargets map[string][]string
}

func (m *objectMeta) attr(name string) (attribute, bool) {
	a, ok := m.bySlug[strings.ToLower(name)]
	return a, ok
}

// numericTypes are compared as numbers when matching, so 1001.00 meets 1001.
var numericTypes = map[string]bool{"number": true, "currency": true, "rating": true}

// listObjects fetches every object in the workspace, cached for the run.
func (d *AttioDestination) listObjects(ctx context.Context) ([]objectInfo, error) {
	d.mu.Lock()
	if d.objects != nil {
		defer d.mu.Unlock()
		return d.objects, nil
	}
	d.mu.Unlock()

	resp, err := d.reads.R(ctx).Get("/objects")
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, parseAPIError(resp)
	}
	var parsed struct {
		Data []objectInfo `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, fmt.Errorf("attio: failed to parse objects: %w", err)
	}
	d.mu.Lock()
	d.objects = parsed.Data
	d.mu.Unlock()
	return parsed.Data, nil
}

// describe resolves an object and its attributes, cached by the name used.
func (d *AttioDestination) describe(ctx context.Context, object string) (*objectMeta, error) {
	key := strings.ToLower(object)
	d.mu.Lock()
	if m, ok := d.metas[key]; ok {
		d.mu.Unlock()
		return m, nil
	}
	d.mu.Unlock()

	resp, err := d.reads.R(ctx).Get("/objects/" + url.PathEscape(object))
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, parseAPIError(resp)
	}
	var obj struct {
		Data objectInfo `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &obj); err != nil {
		return nil, fmt.Errorf("attio: failed to parse object %s: %w", object, err)
	}

	resp, err = d.reads.R(ctx).Get("/objects/" + url.PathEscape(obj.Data.APISlug) + "/attributes")
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, parseAPIError(resp)
	}
	var attrs struct {
		Data []attribute `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &attrs); err != nil {
		return nil, fmt.Errorf("attio: failed to parse attributes of %s: %w", object, err)
	}

	slugByID := map[string]string{}
	if objects, err := d.listObjects(ctx); err == nil {
		for _, o := range objects {
			slugByID[o.ID.ObjectID] = o.APISlug
		}
	}

	m := &objectMeta{slug: obj.Data.APISlug, bySlug: map[string]attribute{}, refTargets: map[string][]string{}}
	for _, a := range attrs.Data {
		if a.IsArchived {
			continue
		}
		k := strings.ToLower(a.APISlug)
		m.bySlug[k] = a
		if a.Type == "record-reference" {
			for _, id := range a.Config.RecordReference.AllowedObjectIDs {
				if s := slugByID[id]; s != "" {
					m.refTargets[k] = append(m.refTargets[k], s)
				} else {
					m.refTargets[k] = append(m.refTargets[k], id)
				}
			}
		}
	}

	d.mu.Lock()
	d.metas[key] = m
	d.mu.Unlock()
	return m, nil
}

// unknownObjectError explains a dest-table that names no object, suggesting the
// api_slug when the name matches an object's display noun ("Person" -> people).
func (d *AttioDestination) unknownObjectError(ctx context.Context, name string) error {
	base := fmt.Sprintf("attio: no object named %q; --dest-table takes the object's API slug (e.g. people, companies, deals) or id", name)
	objects, err := d.listObjects(ctx)
	if err != nil {
		return errors.New(base)
	}
	var matches, slugs []string
	for _, o := range objects {
		slugs = append(slugs, o.APISlug)
		if strings.EqualFold(o.APISlug, name) || strings.EqualFold(o.SingularNoun, name) || strings.EqualFold(o.PluralNoun, name) {
			matches = append(matches, o.APISlug)
		}
	}
	if len(matches) > 0 {
		return fmt.Errorf("%s — did you mean %s?", base, strings.Join(matches, ", "))
	}
	slices.Sort(slugs)
	return fmt.Errorf("%s (available: %s)", base, strings.Join(slugs, ", "))
}

// PrepareTable validates the object, the match attribute, and every source column
// against the object's attributes before any record is written.
func (d *AttioDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return nil
	}
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), "", false)
	if err != nil {
		return err
	}
	if len(sh.labelColumns) > 0 {
		output.Warnf("Warning: append always creates new %s records and never matches on the primary key [%s]; it only labels rejected rows. Use --incremental-strategy merge with matching_attribute=<attribute> to update existing records\n", sh.object, strings.Join(sh.labelColumns, ", "))
	}

	meta, err := d.describe(ctx, sh.object)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.status == 404 {
			return d.unknownObjectError(ctx, sh.object)
		}
		if errors.As(err, &apiErr) && (apiErr.status == 401 || apiErr.status == 403) {
			return fmt.Errorf("attio: cannot read the %s object: %w; the access token needs the object_configuration:read scope", sh.object, err)
		}
		output.Warnf("Warning: attio could not describe %s (%v); skipping column validation\n", sh.object, err)
		return nil
	}

	if sh.matchAttr != "" && sh.matchAttr != recordIDAttr {
		a, ok := meta.attr(sh.matchAttr)
		if !ok {
			return fmt.Errorf("attio: matching_attribute %q is not an attribute on %s (unique attributes: %s)", sh.matchAttr, meta.slug, strings.Join(uniqueAttributes(meta), ", "))
		}
		if sh.upsert() && !a.IsUnique {
			return fmt.Errorf("attio: matching_attribute %q on %s is not unique; Attio can only upsert on a unique attribute (unique attributes: %s). Use --incremental-strategy update to update every record that matches it", a.APISlug, meta.slug, strings.Join(uniqueAttributes(meta), ", "))
		}
		if a.Type == "record-reference" || a.Type == "actor-reference" || a.Type == "interaction" || a.Type == "location" || a.Type == "personal-name" {
			return fmt.Errorf("attio: cannot match on %s attribute %q; use a text, number, email, domain or other unique attribute", a.Type, a.APISlug)
		}
	}
	if sh.archive {
		return nil
	}

	var unknown, readOnly, badRef []string
	for _, col := range opts.Schema.Columns {
		name := col.Name
		if sh.skipColumn(name) {
			continue
		}
		if rel, typ, field, ok := dottedColumn(name); ok {
			a, known := meta.attr(rel)
			if !known {
				unknown = append(unknown, name)
				continue
			}
			if !a.IsWritable {
				readOnly = append(readOnly, name)
				continue
			}
			switch a.Type {
			case "personal-name":
				if typ != "" || !nameFields[strings.ToLower(field)] {
					badRef = append(badRef, fmt.Sprintf("%s (name columns are %s.first_name, %s.last_name or %s.full_name)", name, rel, rel, rel))
				}
			case "record-reference":
				targets := meta.refTargets[strings.ToLower(rel)]
				switch {
				case typ == "" && len(targets) != 1:
					badRef = append(badRef, fmt.Sprintf("%s (%s can point to %s; name the object, e.g. %s.%s.%s)", name, rel, strings.Join(targets, ", "), rel, firstOr(targets, "companies"), field))
				case typ != "" && len(targets) > 0 && !slices.ContainsFunc(targets, func(t string) bool { return strings.EqualFold(t, typ) }):
					badRef = append(badRef, fmt.Sprintf("%s (%s can point to %s)", name, rel, strings.Join(targets, ", ")))
				default:
					if msg := d.checkLinkField(ctx, typ, targets, field); msg != "" {
						badRef = append(badRef, fmt.Sprintf("%s (%s)", name, msg))
					}
				}
			default:
				badRef = append(badRef, fmt.Sprintf("%s (%s is a %s attribute, not a record reference)", name, rel, a.Type))
			}
			continue
		}
		a, ok := meta.attr(name)
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if !a.IsWritable {
			readOnly = append(readOnly, name)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("attio: source columns are not attributes on %s: [%s]; create them in Attio, rename them onto existing attributes with --columns, or drop them from the source (e.g. --sql-exclude-columns)", meta.slug, strings.Join(unknown, ", "))
	}
	if len(readOnly) > 0 {
		return fmt.Errorf("attio: source columns are read-only attributes on %s: [%s]; drop them from the source (e.g. --sql-exclude-columns)", meta.slug, strings.Join(readOnly, ", "))
	}
	if len(badRef) > 0 {
		return fmt.Errorf("attio: invalid dotted columns on %s: %s", meta.slug, strings.Join(badRef, "; "))
	}
	return nil
}

// checkLinkField reports why field can't identify the linked record: Attio
// finds it only by record_id or a unique attribute.
func (d *AttioDestination) checkLinkField(ctx context.Context, typ string, targets []string, field string) string {
	if strings.EqualFold(field, recordIDAttr) {
		return ""
	}
	object := typ
	if object == "" {
		object = firstOr(targets, "")
	}
	target, err := d.describe(ctx, object)
	if err != nil {
		return ""
	}
	a, ok := target.attr(field)
	switch {
	case !ok:
		return fmt.Sprintf("%s has no attribute %s", target.slug, field)
	case !a.IsUnique:
		return fmt.Sprintf("%s is not unique on %s; link by record_id or a unique attribute (%s)", a.APISlug, target.slug, strings.Join(uniqueAttributes(target), ", "))
	}
	return ""
}

func uniqueAttributes(m *objectMeta) []string {
	var out []string
	for _, a := range m.bySlug {
		if a.IsUnique {
			out = append(out, a.APISlug)
		}
	}
	if len(out) == 0 {
		return []string{"none defined"}
	}
	slices.Sort(out)
	return out
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}
