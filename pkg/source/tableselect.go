package source

import (
	"fmt"
	"sort"
	"strings"
)

// TableSelector is an optional capability for multi-table sources that can
// restrict their capture set to a user-supplied list of tables. The pipeline
// calls SelectTables once, right after Connect, so the selection is in place
// before any table discovery runs -- including a source's own re-listing
// inside ReadAll and any periodic new-table check.
type TableSelector interface {
	SelectTables(names []string) error
}

// TableSelectionOptions describes how a source matches and reports table names.
type TableSelectionOptions struct {
	// Subject names one table the way the source refers to it, e.g.
	// "SQL Server CDC table" or "MongoDB collection".
	Subject string
	// Scope names the set being searched, e.g. "SQL Server CDC-enabled tables".
	Scope string
	// Hint is optional trailing guidance for a not-found error, e.g.
	// "use schema-qualified names (e.g. dbo.users)".
	Hint string
	// Canonicalize rewrites a user-supplied name into the form the source uses
	// for SourceTableInfo.Name. It may reject a malformed name.
	Canonicalize func(string) (string, error)
}

// requestedTable is one entry of the user's list, kept in both spellings: the
// canonical form is what discovered names are compared against, the raw form is
// what diagnostics quote back.
type requestedTable struct {
	raw       string
	canonical string
}

// TableSelection is an immutable, canonicalized set of requested tables.
//
// Every method is pure and a nil selection means "all tables", so a source can
// consult one selection from its discovery timer and its stream-rebuild path
// concurrently without synchronisation.
type TableSelection struct {
	opts  TableSelectionOptions
	byKey map[string]requestedTable // folded canonical name -> request
}

// NewTableSelection canonicalizes and validates the requested names. It returns
// a nil selection when nothing was requested, which every method treats as
// "select everything".
func NewTableSelection(requested []string, opts TableSelectionOptions) (*TableSelection, error) {
	byKey := make(map[string]requestedTable, len(requested))
	for _, raw := range requested {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		canonical := raw
		if opts.Canonicalize != nil {
			rewritten, err := opts.Canonicalize(raw)
			if err != nil {
				return nil, err
			}
			canonical = rewritten
		}
		key := strings.ToLower(canonical)
		// Two spellings can collapse only after canonicalization (Postgres
		// "public.users" and "users"), so name both to make the fix obvious.
		if previous, ok := byKey[key]; ok {
			if previous.raw == raw {
				return nil, fmt.Errorf("%s %s is listed more than once", opts.Subject, raw)
			}
			return nil, fmt.Errorf("%s %s and %s name the same table", opts.Subject, previous.raw, raw)
		}
		byKey[key] = requestedTable{raw: raw, canonical: canonical}
	}
	if len(byKey) == 0 {
		return nil, nil
	}
	return &TableSelection{opts: opts, byKey: byKey}, nil
}

// Empty reports whether the selection covers every table.
func (s *TableSelection) Empty() bool {
	return s == nil || len(s.byKey) == 0
}

// Resolve maps a source's discovered inventory onto the requested subset,
// returning the names to ingest.
//
// Matching is case-insensitive so a user need not reproduce the catalog's
// casing, but an exact spelling always wins: a source holding both "users" and
// "Users" ingests only the one that was named. A request that matches several
// discovered names and none of them exactly is genuinely ambiguous, and
// resolving fails rather than quietly ingesting all of them.
func (s *TableSelection) Resolve(inventory []string) (map[string]struct{}, error) {
	selected := make(map[string]struct{}, len(inventory))
	if s.Empty() {
		for _, name := range inventory {
			selected[name] = struct{}{}
		}
		return selected, nil
	}

	matches := make(map[string][]string, len(s.byKey))
	for _, name := range inventory {
		key := strings.ToLower(name)
		if _, ok := s.byKey[key]; ok {
			matches[key] = append(matches[key], name)
		}
	}

	keys := make([]string, 0, len(matches))
	for key := range matches {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		found := matches[key]
		request := s.byKey[key]
		if len(found) == 1 {
			selected[found[0]] = struct{}{}
			continue
		}
		exact := ""
		for _, name := range found {
			if name == request.canonical {
				exact = name
				break
			}
		}
		if exact == "" {
			sorted := append([]string(nil), found...)
			sort.Strings(sorted)
			return nil, fmt.Errorf("%s %s matches several tables that differ only in case (%s); name one of them exactly",
				s.opts.Subject, request.raw, strings.Join(sorted, ", "))
		}
		selected[exact] = struct{}{}
	}
	return selected, nil
}

// Validate reports requested tables that the source cannot ingest. present is
// what the source actually discovered for this selection; ineligible maps a
// present-but-unusable table to the reason it was excluded (no primary key,
// unlogged, ...).
//
// A requested name that matches nothing is an error rather than a silent
// no-op: ingesting nothing would hide a typo, and in streaming mode the run
// would poll forever.
func (s *TableSelection) Validate(present []string, ineligible map[string]string) error {
	if s.Empty() {
		return nil
	}
	found := make(map[string]struct{}, len(present))
	for _, name := range present {
		found[strings.ToLower(name)] = struct{}{}
	}
	reasons := make(map[string]string, len(ineligible))
	for name, reason := range ineligible {
		reasons[strings.ToLower(name)] = reason
	}

	missing := make([]string, 0, len(s.byKey))
	for key, request := range s.byKey {
		if _, ok := found[key]; ok {
			continue
		}
		if reason, ok := reasons[key]; ok {
			return fmt.Errorf("%s %s cannot be ingested: %s", s.opts.Subject, request.raw, reason)
		}
		missing = append(missing, request.raw)
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	message := fmt.Sprintf("tables not found among %s: %s", s.opts.Scope, strings.Join(missing, ", "))
	if s.opts.Hint != "" {
		message += "; " + s.opts.Hint
	}
	return fmt.Errorf("%s", message)
}
