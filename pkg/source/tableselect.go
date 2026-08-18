package source

import (
	"fmt"
	"sort"
	"strings"
)

// TableSelector is an optional capability for multi-table sources that can
// restrict their capture set to a user-supplied list of tables. The pipeline
// calls SelectTables once, right after Connect, so the selection is in place
// before any table discovery runs.
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

// TableSelection is an immutable, canonicalized set of requested tables.
//
// Every method is pure and a nil selection means "all tables", so a source can
// consult one selection from its discovery timer and its stream-rebuild path
// concurrently without synchronisation.
type TableSelection struct {
	opts  TableSelectionOptions
	byKey map[string]string // folded canonical name -> the user's original spelling
}

// NewTableSelection canonicalizes and validates the requested names. It returns
// a nil selection when nothing was requested, which every method treats as
// "select everything".
func NewTableSelection(requested []string, opts TableSelectionOptions) (*TableSelection, error) {
	byKey := make(map[string]string, len(requested))
	for _, raw := range requested {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name := raw
		if opts.Canonicalize != nil {
			canonical, err := opts.Canonicalize(raw)
			if err != nil {
				return nil, err
			}
			name = canonical
		}
		key := strings.ToLower(name)
		// Two spellings can collapse only after canonicalization (Postgres
		// "public.users" and "users"), so name both to make the fix obvious.
		if previous, ok := byKey[key]; ok {
			if previous == raw {
				return nil, fmt.Errorf("%s %s is listed more than once", opts.Subject, raw)
			}
			return nil, fmt.Errorf("%s %s and %s name the same table", opts.Subject, previous, raw)
		}
		byKey[key] = raw
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

// Includes reports whether name was requested. A nil selection includes
// everything.
func (s *TableSelection) Includes(name string) bool {
	if s.Empty() {
		return true
	}
	_, ok := s.byKey[strings.ToLower(name)]
	return ok
}

// FilterNames returns the requested subset of all, preserving order.
func (s *TableSelection) FilterNames(all []string) []string {
	if s.Empty() {
		return all
	}
	filtered := make([]string, 0, len(s.byKey))
	for _, name := range all {
		if s.Includes(name) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// FilterTables returns the requested subset of all, preserving order.
func (s *TableSelection) FilterTables(all []SourceTableInfo) []SourceTableInfo {
	if s.Empty() {
		return all
	}
	filtered := make([]SourceTableInfo, 0, len(s.byKey))
	for _, table := range all {
		if s.Includes(table.Name) {
			filtered = append(filtered, table)
		}
	}
	return filtered
}

// Validate reports requested tables that the source cannot ingest. present is
// the full inventory the source discovered; ineligible maps a present-but-
// unusable table to the reason it was excluded (no primary key, unlogged, …).
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
	for key, raw := range s.byKey {
		if _, ok := found[key]; ok {
			continue
		}
		if reason, ok := reasons[key]; ok {
			return fmt.Errorf("%s %s cannot be ingested: %s", s.opts.Subject, raw, reason)
		}
		missing = append(missing, raw)
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
