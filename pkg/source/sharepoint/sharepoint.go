// Package sharepoint implements an ingestr source that reads files (xlsx, csv)
// from a SharePoint Online document library via the Microsoft Graph
// API. It supports globbing, multi-sheet Excel reads, and lands raw all-VARCHAR
// rows alongside metadata columns (_source_file, _sheet_name, _row_idx) that
// record each row's source file, sheet, and read order.
package sharepoint

import (
	"context"
	"fmt"
	"net/url"
	"os"
	gopath "path"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

// Metadata columns emitted on every row.
const (
	colSourceFile = "_source_file" // file path within the document library
	colSheetName  = "_sheet_name"  // Excel sheet tab name (null for csv)
	colRowIdx     = "_row_idx"     // 0-based row position in read order, post-skip/header
)

type fileFormat string

const (
	formatUnknown fileFormat = ""
	formatXLSX    fileFormat = "xlsx"
	formatXLS     fileFormat = "xls"
	formatCSV     fileFormat = "csv"
)

// defaultMaxFiles bounds how many files a glob may match before erroring, to
// guard against an over-broad recursive pattern enumerating a whole library.
const defaultMaxFiles = 10000

// connConfig holds the connection parameters parsed from the source URI.
type connConfig struct {
	tenantID     string
	clientID     string
	clientSecret string
	hostname     string
	sitePath     string
	library      string // optional document library name; empty => default ("Documents")
	maxFileSize  int64  // optional max bytes per downloaded file; 0 => unlimited
	maxFiles     int    // optional max files a glob may match; 0 => unlimited
}

// tableSpec is the parsed form of a source-table string.
type tableSpec struct {
	path      string
	format    fileFormat
	sheets    []string // empty => first-sheet fallback (Excel)
	skip      int
	encoding  string
	sep       string
	formatted bool     // Excel: use formatted cell text instead of raw values
	dropEmpty bool     // skip rows whose data columns are all empty
	dateCols  []string // Excel: column names whose serial values are converted to ISO dates
}

type SharePointSource struct {
	graph *graphClient
}

func NewSharePointSource() *SharePointSource {
	return &SharePointSource{}
}

func (s *SharePointSource) Schemes() []string {
	return []string{"sharepoint"}
}

func (s *SharePointSource) HandlesIncrementality() bool {
	return false
}

func (s *SharePointSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	g, err := newGraphClient(cfg)
	if err != nil {
		return err
	}
	if err := g.connect(ctx); err != nil {
		return err
	}

	s.graph = g
	config.Debug("[SHAREPOINT] connected to site %q on %q", cfg.sitePath, cfg.hostname)
	return nil
}

func (s *SharePointSource) Close(ctx context.Context) error {
	if s.graph != nil {
		return s.graph.close()
	}
	return nil
}

func (s *SharePointSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}

	// Honor the user-requested strategy; default to replace. Unlike Google
	// Sheets, this source does not hard-code replace, so append / merge /
	// delete+insert are all selectable. append re-reads everything each run and
	// duplicates by design — there is no incremental filtering.
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("sharepoint source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spec, opts)
		},
	}, nil
}

func (s *SharePointSource) read(ctx context.Context, spec tableSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		paths, err := s.graph.listMatching(ctx, spec.path)
		if err != nil {
			_ = send(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("failed to list files for %q: %w", spec.path, err)})
			return
		}
		if len(paths) == 0 {
			_ = send(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("no files found matching %q", spec.path)})
			return
		}

		total := 0
		for _, path := range paths {
			if ctx.Err() != nil {
				return
			}

			if err := s.readFile(ctx, path, spec, opts, results, &total); err != nil {
				_ = send(ctx, results, source.RecordBatchResult{Err: err})
				return
			}

			if opts.Limit > 0 && total >= opts.Limit {
				break
			}
		}
	}()

	return results, nil
}

// readFile downloads one file to a local temp file and dispatches it to the
// format-specific reader. The temp file is always removed when done.
func (s *SharePointSource) readFile(ctx context.Context, path string, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult, total *int) error {
	tmp, err := os.CreateTemp("", "ingestr-sharepoint-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %q: %w", path, err)
	}
	localPath := tmp.Name()
	_ = tmp.Close() // downloadToFile re-creates it; we only needed a unique path
	defer func() { _ = os.Remove(localPath) }()

	if err := s.graph.downloadToFile(ctx, path, localPath); err != nil {
		return err
	}

	// spec.format is set for a literal path or a glob with an extension; for an
	// extensionless glob it is detected per matched file here.
	format := spec.format
	if format == formatUnknown {
		format = detectFormat(path)
	}
	switch format {
	case formatXLSX:
		return readExcel(ctx, path, localPath, spec, opts, results, total)
	case formatCSV:
		return readCSV(ctx, path, localPath, spec, opts, results, total)
	case formatXLS:
		return fmt.Errorf("legacy .xls (BIFF) files are not supported for %q; re-save the file as .xlsx", path)
	default:
		return fmt.Errorf("could not determine file format for %q; add a format hint such as #xlsx or #csv", path)
	}
}

func parseURI(uri string) (connConfig, error) {
	if !strings.HasPrefix(uri, "sharepoint://") {
		return connConfig{}, fmt.Errorf("invalid sharepoint URI: must start with sharepoint://")
	}

	u, err := url.Parse(uri)
	if err != nil {
		return connConfig{}, fmt.Errorf("failed to parse sharepoint URI: %w", err)
	}

	q := u.Query()
	cfg := connConfig{
		tenantID:     q.Get("tenant_id"),
		clientID:     q.Get("client_id"),
		clientSecret: q.Get("client_secret"),
		hostname:     q.Get("hostname"),
		sitePath:     strings.Trim(q.Get("site"), "/"),
		library:      strings.TrimSpace(q.Get("library")),
		maxFiles:     defaultMaxFiles,
	}

	if v := strings.TrimSpace(q.Get("max_file_size")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return connConfig{}, fmt.Errorf("invalid max_file_size %q: must be a non-negative integer (bytes)", v)
		}
		cfg.maxFileSize = n
	}
	if v := strings.TrimSpace(q.Get("max_files")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return connConfig{}, fmt.Errorf("invalid max_files %q: must be a non-negative integer (0 = unlimited)", v)
		}
		cfg.maxFiles = n
	}

	missing := make([]string, 0, 5)
	if cfg.tenantID == "" {
		missing = append(missing, "tenant_id")
	}
	if cfg.clientID == "" {
		missing = append(missing, "client_id")
	}
	if cfg.clientSecret == "" {
		missing = append(missing, "client_secret")
	}
	if cfg.hostname == "" {
		missing = append(missing, "hostname")
	}
	if cfg.sitePath == "" {
		missing = append(missing, "site")
	}
	if len(missing) > 0 {
		return connConfig{}, fmt.Errorf("sharepoint URI is missing required parameter(s): %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// sharepointParamKeys are the query parameters recognized by the URL-style
// source-table form (see parseTableSpec). Kept as the single source of truth for
// strict validation so a typo errors instead of being silently dropped.
var sharepointParamKeys = []string{
	"sheet", "sheets", "skip", "encoding", "sep", "format", "raw", "formatted", "drop_empty", "date_cols",
}

// parseTableSpec parses a source-table string in one of two forms:
//
//	<path>?<key>=<val>&...           (URL-style query parameters; preferred)
//	<path>#<format>,<key>=<val>,...  (legacy comma-separated hints)
//
// The path is literal (may contain spaces and "&") and may include glob
// wildcards (* ? [ ] {}). The query form is selected only when the text after the
// last "?" looks like a parameter block, so a "?" used as a glob wildcard (e.g.
// "Reports/q?.xlsx") stays part of the path; otherwise the legacy form applies and
// the string is split on the LAST "#" only when the suffix parses as a hint list.
// Use "%23" to embed a literal "#" in the path, and percent-encode any literal "?"
// that must sit in the path alongside query parameters.
func parseTableSpec(name string) (tableSpec, error) {
	spec := tableSpec{}

	path, params, hasQuery, err := tablespec.Split(name)
	if err != nil {
		return tableSpec{}, err
	}
	if hasQuery {
		if err := tablespec.ValidateKeys(params, sharepointParamKeys...); err != nil {
			return tableSpec{}, err
		}
		if err := applyParams(&spec, params); err != nil {
			return tableSpec{}, err
		}
	} else {
		path = name
		if idx := strings.LastIndex(name, "#"); idx != -1 {
			suffix := name[idx+1:]
			if looksLikeHints(suffix) {
				path = name[:idx]
				if err := applyHints(&spec, suffix); err != nil {
					return tableSpec{}, err
				}
			}
		}
	}

	path = strings.ReplaceAll(path, "%23", "#")
	path = strings.TrimSpace(path)
	if path == "" {
		return tableSpec{}, fmt.Errorf("sharepoint source-table path is empty")
	}
	spec.path = path

	if spec.format == formatUnknown {
		spec.format = detectFormat(path)
	}
	// A glob whose pattern has no usable extension (e.g. "Reports/**") leaves the
	// format unknown here; detection is deferred to each matched file at read time
	// (which also lets one glob span mixed file types). A single literal path must
	// resolve to a format now.
	if spec.format == formatUnknown && !hasGlobMeta(path) {
		return tableSpec{}, fmt.Errorf("could not determine file format for %q; add a format hint such as #xlsx or #csv", path)
	}
	if spec.format == formatXLS {
		return tableSpec{}, fmt.Errorf("legacy .xls (BIFF) files are not supported for %q; re-save the file as .xlsx", path)
	}

	return spec, nil
}

// looksLikeHints reports whether s (the part after the last "#") is a valid,
// comma-separated hint list. It guards the last-"#" split so filenames that
// legitimately contain "#" are not mistaken for hints.
func looksLikeHints(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, tok := range strings.Split(s, ",") {
		if !isValidHint(strings.TrimSpace(tok)) {
			return false
		}
	}
	return true
}

// Recognized hints, kept as single source of truth so the validity check
// (looksLikeHints) and the application (applyHints) cannot drift apart.
var (
	hintKeys  = map[string]bool{"sheet": true, "sheets": true, "skip": true, "encoding": true, "sep": true, "format": true, "date_cols": true}
	hintFlags = map[string]bool{"xlsx": true, "xlsm": true, "xls": true, "csv": true, "raw": true, "formatted": true, "drop_empty": true}
)

func isValidHint(tok string) bool {
	if tok == "" {
		return false
	}
	if eq := strings.Index(tok, "="); eq > 0 {
		return hintKeys[strings.ToLower(strings.TrimSpace(tok[:eq]))]
	}
	return hintFlags[strings.ToLower(tok)]
}

func applyHints(spec *tableSpec, s string) error {
	for _, raw := range strings.Split(s, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}

		if eq := strings.Index(tok, "="); eq > 0 {
			key := strings.ToLower(strings.TrimSpace(tok[:eq]))
			val := strings.TrimSpace(tok[eq+1:])
			switch key {
			case "sheet":
				if val != "" {
					spec.sheets = []string{val}
				}
			case "sheets":
				spec.sheets = splitSheets(val)
			case "skip":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					return fmt.Errorf("invalid skip hint %q: must be a non-negative integer", val)
				}
				spec.skip = n
			case "encoding":
				spec.encoding = val
			case "sep":
				spec.sep = val
			case "format":
				spec.format = parseFormat(val)
			case "date_cols":
				spec.dateCols = splitSheets(val)
			}
			continue
		}

		switch strings.ToLower(tok) {
		case "xlsx", "xlsm", "xls", "csv":
			spec.format = parseFormat(tok)
		case "raw":
			spec.formatted = false
		case "formatted":
			spec.formatted = true
		case "drop_empty":
			spec.dropEmpty = true
		}
	}
	return nil
}

// applyParams applies the URL-style query parameters to spec. Keys are read
// explicitly (not by ranging the map) so behavior is deterministic regardless of
// query order. A list-valued option accepts either a repeated key
// (sheets=A&sheets=B) or a single "|"-joined value (sheets=A|B). Unknown keys are
// rejected earlier by tablespec.ValidateKeys.
func applyParams(spec *tableSpec, p url.Values) error {
	for _, v := range p["sheet"] {
		if v = strings.TrimSpace(v); v != "" {
			spec.sheets = append(spec.sheets, v)
		}
	}
	for _, v := range p["sheets"] {
		spec.sheets = append(spec.sheets, splitSheets(v)...)
	}
	for _, v := range p["date_cols"] {
		spec.dateCols = append(spec.dateCols, splitSheets(v)...)
	}

	if p.Has("skip") {
		n, err := strconv.Atoi(strings.TrimSpace(p.Get("skip")))
		if err != nil || n < 0 {
			return fmt.Errorf("invalid skip parameter %q: must be a non-negative integer", p.Get("skip"))
		}
		spec.skip = n
	}
	if p.Has("encoding") {
		spec.encoding = strings.TrimSpace(p.Get("encoding"))
	}
	if p.Has("sep") {
		spec.sep = p.Get("sep")
	}
	if p.Has("format") {
		spec.format = parseFormat(p.Get("format"))
	}
	if p.Has("formatted") {
		b, err := parseParamBool("formatted", p.Get("formatted"))
		if err != nil {
			return err
		}
		spec.formatted = b
	}
	if p.Has("raw") {
		b, err := parseParamBool("raw", p.Get("raw"))
		if err != nil {
			return err
		}
		if b {
			spec.formatted = false
		}
	}
	if p.Has("drop_empty") {
		b, err := parseParamBool("drop_empty", p.Get("drop_empty"))
		if err != nil {
			return err
		}
		spec.dropEmpty = b
	}

	return nil
}

// parseParamBool reads a boolean query parameter. A bare key (empty value, e.g.
// "?raw") is treated as true to keep flag ergonomics; otherwise the value must
// parse as a Go boolean.
func parseParamBool(key, val string) (bool, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return true, nil
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return false, fmt.Errorf("invalid %s parameter %q: expected a boolean (true/false)", key, val)
	}
	return b, nil
}

// splitSheets splits a sheets= union on "|" (since "," separates hints).
func splitSheets(val string) []string {
	parts := strings.Split(val, "|")
	sheets := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			sheets = append(sheets, p)
		}
	}
	return sheets
}

func parseFormat(s string) fileFormat {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "xlsx", "xlsm":
		return formatXLSX
	case "xls":
		return formatXLS
	case "csv":
		return formatCSV
	default:
		return formatUnknown
	}
}

// detectFormat infers the format from the file extension. It still recognizes
// .xls so parseTableSpec can emit a targeted "re-save as .xlsx" error rather
// than a generic "unknown format" one.
func detectFormat(p string) fileFormat {
	return parseFormat(strings.TrimPrefix(gopath.Ext(p), "."))
}

var _ source.Source = (*SharePointSource)(nil)
