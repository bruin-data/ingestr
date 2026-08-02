package onelake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// deltaSnapshot is the reconstructed state of a Delta table from its log.
type deltaSnapshot struct {
	exists              bool
	version             int64    // latest commit version
	firstVersion        int64    // oldest commit still in the log; not 0 when cleanup truncated it
	activeFiles         []string // data file paths relative to the table directory
	deletionVectorFiles []string // active files whose add action carries a deletion vector
	metadata            deltaMetadata
	metadataVersion     int64 // version of the commit metadata was read from
	protocol            *deltaProtocol
	protocolVersion     int64 // version of the commit protocol was read from
}

// readDeltaSnapshot replays the Delta transaction log under tableDir and returns
// the set of currently-active data files.
func readDeltaSnapshot(ctx context.Context, client dataLakeClient, fileSystem, tableDir string) (*deltaSnapshot, error) {
	logDir := tableDir + "/_delta_log"
	versions, err := client.ListLogVersions(ctx, fileSystem, logDir)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return &deltaSnapshot{exists: false}, nil
	}

	active := make(map[string]bool)
	order := make([]string, 0)
	snapshot := &deltaSnapshot{exists: true, firstVersion: versions[0]}

	for _, v := range versions {
		data, err := client.Download(ctx, fileSystem, logDir+"/"+commitFileName(v))
		if err != nil {
			return nil, fmt.Errorf("failed to read delta commit %d: %w", v, err)
		}
		if err := replayDeltaCommit(snapshot, active, &order, data); err != nil {
			return nil, fmt.Errorf("failed to parse delta commit %d: %w", v, err)
		}
	}

	files := make([]string, 0, len(active))
	for _, p := range order {
		if deletionVector, ok := active[p]; ok {
			files = append(files, p)
			if deletionVector {
				snapshot.deletionVectorFiles = append(snapshot.deletionVectorFiles, p)
			}
		}
	}

	snapshot.version = versions[len(versions)-1]
	snapshot.activeFiles = files
	return snapshot, nil
}

// deltaMetadataCache is the result of an earlier metadata scan of a table's
// log: the metaData and protocol actions that were in effect at version, and
// the versions whose commits carried them. protocolVersion is -1 when the
// scanned log carried no protocol action at all.
type deltaMetadataCache struct {
	version         int64
	metadataVersion int64
	protocolVersion int64
	metadata        deltaMetadata
}

// readDeltaMetadata returns the table's latest version plus its metaData and
// protocol actions without replaying the whole log. Both actions fully replace
// their predecessor, so the newest commit that carries one wins and the scan
// runs backwards. ingestr only emits them in the version-0 commit though, so on
// an append-only table that scan still reaches version 0 — pass a cached result
// to bound it to the commits appended since, which is what keeps the pipeline's
// repeated schema lookups off the whole log.
//
// Skipping commits is only sound while the log is the same one the cache was
// taken from, so the commits the cached actions came from are always re-read: a
// table another writer dropped and recreated restarts its log, and its commit
// at the cached metaData version carries different metadata, or none at all.
func readDeltaMetadata(ctx context.Context, client dataLakeClient, fileSystem, tableDir string, cached *deltaMetadataCache) (*deltaSnapshot, error) {
	logDir := tableDir + "/_delta_log"
	versions, err := client.ListLogVersions(ctx, fileSystem, logDir)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return &deltaSnapshot{exists: false}, nil
	}
	latest := versions[len(versions)-1]
	if cached != nil && (len(cached.metadata) == 0 || cached.version > latest) {
		cached = nil
	}

	snapshot := &deltaSnapshot{exists: true, version: latest, firstVersion: versions[0]}
	for i := len(versions) - 1; i >= 0; i-- {
		version := versions[i]
		if cached != nil && version <= cached.version && version != cached.metadataVersion && version != cached.protocolVersion {
			// Delta commits are immutable, so this one held neither action when
			// the cache entry was taken and still holds none.
			continue
		}
		data, err := client.Download(ctx, fileSystem, logDir+"/"+commitFileName(version))
		if err != nil {
			return nil, fmt.Errorf("failed to read delta commit %d: %w", version, err)
		}
		// The scan can continue past the newest metaData while it looks for the
		// protocol action, so each commit replays into its own snapshot and the
		// newest occurrence of each action wins.
		var commit deltaSnapshot
		if err := replayDeltaCommit(&commit, map[string]bool{}, &[]string{}, data); err != nil {
			return nil, fmt.Errorf("failed to parse delta commit %d: %w", version, err)
		}
		if cached != nil && version == cached.metadataVersion && !sameDeltaMetadata(commit.metadata, cached.metadata) {
			return readDeltaMetadata(ctx, client, fileSystem, tableDir, nil)
		}
		if len(snapshot.metadata) == 0 && len(commit.metadata) > 0 {
			snapshot.metadata = commit.metadata
			snapshot.metadataVersion = version
		}
		if snapshot.protocol == nil && commit.protocol != nil {
			snapshot.protocol = commit.protocol
			snapshot.protocolVersion = version
		}
		if len(snapshot.metadata) > 0 && snapshot.protocol != nil {
			break
		}
	}
	return snapshot, nil
}

func sameDeltaMetadata(left, right deltaMetadata) bool {
	encodedLeft, err := json.Marshal(left)
	if err != nil {
		return false
	}
	encodedRight, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func replayDeltaCommit(snapshot *deltaSnapshot, active map[string]bool, order *[]string, data []byte) error {
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var action struct {
			Add *struct {
				Path           string          `json:"path"`
				DeletionVector json.RawMessage `json:"deletionVector"`
			} `json:"add"`
			Remove   *struct{ Path string } `json:"remove"`
			MetaData deltaMetadata          `json:"metaData"`
			Protocol *deltaProtocol         `json:"protocol"`
		}
		if err := json.Unmarshal([]byte(line), &action); err != nil {
			return err
		}
		switch {
		case action.Add != nil:
			if _, ok := active[action.Add.Path]; !ok {
				*order = append(*order, action.Add.Path)
			}
			active[action.Add.Path] = jsonValuePresent(action.Add.DeletionVector)
		case action.Remove != nil:
			delete(active, action.Remove.Path)
		case action.MetaData != nil:
			snapshot.metadata = action.MetaData
		case action.Protocol != nil:
			snapshot.protocol = action.Protocol
		}
	}
	return nil
}

func jsonValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// readDeltaData downloads and decodes all active data files of a Delta table into
// Arrow record batches. The caller owns the returned batches and must Release them.
func readDeltaData(ctx context.Context, client dataLakeClient, fileSystem, tableDir string, files []string, tableSchema *deltaStruct) ([]arrow.RecordBatch, error) {
	var batches []arrow.RecordBatch
	for _, f := range files {
		data, err := client.Download(ctx, fileSystem, tableDir+"/"+f)
		if err != nil {
			releaseBatches(batches)
			return nil, err
		}
		b, err := readParquetBytes(ctx, data)
		if err != nil {
			releaseBatches(batches)
			return nil, fmt.Errorf("failed to read parquet %s: %w", f, err)
		}
		batches = append(batches, b...)
	}
	aligned, err := alignDeltaBatches(batches, tableSchema)
	if err != nil {
		releaseBatches(batches)
		return nil, err
	}
	releaseBatches(batches)
	return aligned, nil
}

// alignDeltaBatches projects every Parquet file to the current Delta schema.
// Older files can omit columns introduced by metadata-only evolution; those
// values are materialized as NULL before copy-on-write strategies read them.
func alignDeltaBatches(batches []arrow.RecordBatch, tableSchema *deltaStruct) ([]arrow.RecordBatch, error) {
	if len(batches) == 0 {
		return nil, nil
	}
	if tableSchema == nil {
		return nil, fmt.Errorf("cannot align delta data without a table schema")
	}

	fields := make([]arrow.Field, 0, len(tableSchema.Fields))
	for _, deltaField := range tableSchema.Fields {
		column, columnErr := columnFromDeltaField(deltaField)
		field, found := latestArrowField(batches, deltaField.Name)
		switch {
		case !found:
			if columnErr != nil {
				// A metadata-only ALTER TABLE can declare a column ingestr
				// cannot map before any file carries it. There is nothing to
				// materialize: the rewrite leaves the metaData action alone, so
				// the column stays declared and readers keep NULL-filling it,
				// the same way tableSchemaFromDelta keeps it out of the write
				// schema.
				config.Debug("[ONELAKE] Skipping delta column %q with no data that ingestr cannot map: %v", deltaField.Name, columnErr)
				continue
			}
			// The fill is entirely NULL, so it has to be nullable no matter what
			// the Delta schema declares.
			field = arrow.Field{Type: schema.DataTypeToArrowType(column), Nullable: true}

		case columnErr == nil && sameDeltaEncoding(field, deltaField.Type):
			field.Type = schema.DataTypeToArrowType(column)
			field.Nullable = field.Nullable || deltaField.Nullable

		default:
			field.Nullable = field.Nullable || deltaField.Nullable
		}
		field.Name = deltaField.Name
		fields = append(fields, field)
	}
	target := arrow.NewSchema(fields, nil)

	aligned := make([]arrow.RecordBatch, 0, len(batches))
	for _, batch := range batches {
		record, err := databuffer.CastRecordToSchema(batch, target, true)
		if err != nil {
			releaseBatches(aligned)
			return nil, fmt.Errorf("failed to align delta data file to table schema: %w", err)
		}
		aligned = append(aligned, record)
	}
	return aligned, nil
}

// unifyRewriteBatches casts the target and staging batches to one Arrow schema.
// A copy-on-write rewrite concatenates data written before and after a schema
// evolution: on the target side an evolved column is a NULL fill typed from the
// lossy Delta metadata and forced nullable, while on the staging side it still
// carries the source's own Arrow type and nullability. declared is the target
// table's Delta column list, or nil when the table does not exist yet. The
// returned release function frees whatever this call allocated; the inputs stay
// owned by the caller.
func unifyRewriteBatches(target, staging []arrow.RecordBatch, declared []string) ([]arrow.RecordBatch, []arrow.RecordBatch, func(), error) {
	noop := func() {}
	if len(staging) == 0 {
		return target, staging, noop, nil
	}
	// A rewrite commit carries no metaData action, so a staging column the table
	// does not declare would be written into the data file and then ignored by
	// every Delta reader. Fail instead of losing it silently — this is what a
	// schema change that evolution could not apply looks like here.
	if declared != nil {
		if missing, ok := columnsNotIn(staging[0].Schema(), declared); !ok {
			return nil, nil, noop, fmt.Errorf(
				"cannot rewrite the table: column %q is not declared in its Delta schema (declared columns are %v)", missing, declared)
		}
	}
	if len(target) == 0 {
		return target, staging, noop, nil
	}
	unified, err := unifiedRewriteSchema(target, staging)
	if err != nil {
		return nil, nil, noop, err
	}
	if unified == nil {
		return target, staging, noop, nil
	}

	castTarget, err := castBatchesToSchema(target, unified)
	if err != nil {
		return nil, nil, noop, err
	}
	castStaging, err := castBatchesToSchema(staging, unified)
	if err != nil {
		releaseBatches(castTarget)
		return nil, nil, noop, err
	}
	return castTarget, castStaging, func() {
		releaseBatches(castTarget)
		releaseBatches(castStaging)
	}, nil
}

// unifiedRewriteSchema returns the schema both sides should be cast to, nil when
// they already agree, or an error when they genuinely conflict — a conflict must
// fail the rewrite rather than be silently reconciled. Columns are matched by
// name, not position: schema evolution appends a new column to the end of the
// Delta schema while the ingest schema keeps its own ordering (SCD2, for
// instance, always keeps the _scd_* columns last).
func unifiedRewriteSchema(target, staging []arrow.RecordBatch) (*arrow.Schema, error) {
	targetSchema := target[0].Schema()
	stagingSchema := staging[0].Schema()
	if missing, ok := columnsNotIn(stagingSchema, arrowFieldNames(targetSchema)); !ok {
		return nil, fmt.Errorf("cannot rewrite the table: column %q is missing from its data files (columns are %v)",
			missing, arrowFieldNames(targetSchema))
	}

	fields := make([]arrow.Field, targetSchema.NumFields())
	for i := 0; i < targetSchema.NumFields(); i++ {
		field := targetSchema.Field(i)
		index, ok := fieldIndex(stagingSchema, field.Name)
		if !ok {
			// A column the source dropped is kept and relaxed to nullable, so
			// the incoming rows get a NULL fill for it. If the table still
			// declares it NOT NULL — evolution was skipped or the contract is
			// not "evolve" — filling it would break that declaration.
			if !field.Nullable {
				return nil, fmt.Errorf(
					"cannot rewrite the table: column %q is required but the incoming rows do not have it", field.Name)
			}
			fields[i] = field
			continue
		}
		other := stagingSchema.Field(index)
		if !arrow.TypeEqual(field.Type, other.Type) {
			return nil, fmt.Errorf("cannot rewrite the table: column %q is %s in the table and %s in the incoming rows",
				field.Name, field.Type, other.Type)
		}
		if other.Nullable {
			field.Nullable = true
		}
		fields[i] = field
	}

	unified := arrow.NewSchema(fields, nil)
	if schemaEqualIgnoringMetadata(unified, targetSchema) && schemaEqualIgnoringMetadata(unified, stagingSchema) {
		return nil, nil
	}
	return unified, nil
}

// columnsNotIn returns the first column of s that names is missing, if any.
func columnsNotIn(s *arrow.Schema, names []string) (string, bool) {
	for i := 0; i < s.NumFields(); i++ {
		name := s.Field(i).Name
		if !slices.ContainsFunc(names, func(other string) bool { return strings.EqualFold(name, other) }) {
			return name, false
		}
	}
	return "", true
}

func castBatchesToSchema(batches []arrow.RecordBatch, target *arrow.Schema) ([]arrow.RecordBatch, error) {
	out := make([]arrow.RecordBatch, 0, len(batches))
	for _, batch := range batches {
		record, err := databuffer.CastRecordToSchema(batch, target, true)
		if err != nil {
			releaseBatches(out)
			return nil, fmt.Errorf("failed to reconcile target and staging schemas during rewrite: %w", err)
		}
		out = append(out, record)
	}
	return out, nil
}

// sameDeltaEncoding reports whether an Arrow field found in a data file encodes
// the Delta type the table schema declares. Delta has no naive timestamp, TIME
// or JSON type, so one Delta type covers several Arrow types and files written
// by different generations of the same table disagree on which one they used.
// Re-deriving the Arrow type from the Delta type puts them back in step.
func sameDeltaEncoding(field arrow.Field, deltaType any) bool {
	fileType, err := json.Marshal(deltaTypeFor(arrowFieldToColumn(field)))
	if err != nil {
		return false
	}
	declared, err := json.Marshal(deltaType)
	if err != nil {
		return false
	}
	return string(fileType) == string(declared)
}

func latestArrowField(batches []arrow.RecordBatch, name string) (arrow.Field, bool) {
	for i := len(batches) - 1; i >= 0; i-- {
		batchSchema := batches[i].Schema()
		for j := 0; j < batchSchema.NumFields(); j++ {
			if strings.EqualFold(batchSchema.Field(j).Name, name) {
				return batchSchema.Field(j), true
			}
		}
	}
	return arrow.Field{}, false
}

// readParquetBytes decodes Parquet bytes into Arrow record batches.
func readParquetBytes(ctx context.Context, data []byte) ([]arrow.RecordBatch, error) {
	pr, err := file.NewParquetReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = pr.Close() }()

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, err
	}

	tbl, err := fr.ReadTable(ctx)
	if err != nil {
		return nil, err
	}
	defer tbl.Release()

	if tbl.NumRows() == 0 {
		return nil, nil
	}

	tr := array.NewTableReader(tbl, tbl.NumRows())
	defer tr.Release()

	var batches []arrow.RecordBatch
	for tr.Next() {
		rec := tr.RecordBatch()
		rec.Retain()
		batches = append(batches, rec)
	}
	if err := tr.Err(); err != nil {
		releaseBatches(batches)
		return nil, err
	}
	return batches, nil
}

func releaseBatches(batches []arrow.RecordBatch) {
	for _, b := range batches {
		if b != nil {
			b.Release()
		}
	}
}
