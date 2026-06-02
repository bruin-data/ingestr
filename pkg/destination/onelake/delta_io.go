package onelake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/adlsutil"
)

// deltaSnapshot is the reconstructed state of a Delta table from its log.
type deltaSnapshot struct {
	exists      bool
	version     int64    // latest commit version
	activeFiles []string // data file paths relative to the table directory
}

// readDeltaSnapshot replays the Delta transaction log under tableDir and returns
// the set of currently-active data files.
func readDeltaSnapshot(ctx context.Context, client *adlsutil.DataLakeClient, fileSystem, tableDir string) (*deltaSnapshot, error) {
	logDir := tableDir + "/_delta_log"
	versions, err := client.ListLogVersions(ctx, fileSystem, logDir)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return &deltaSnapshot{exists: false}, nil
	}

	active := make(map[string]struct{})
	order := make([]string, 0)

	for _, v := range versions {
		data, err := client.Download(ctx, fileSystem, logDir+"/"+commitFileName(v))
		if err != nil {
			return nil, fmt.Errorf("failed to read delta commit %d: %w", v, err)
		}
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var action struct {
				Add    *struct{ Path string } `json:"add"`
				Remove *struct{ Path string } `json:"remove"`
			}
			if err := json.Unmarshal([]byte(line), &action); err != nil {
				return nil, fmt.Errorf("failed to parse delta commit %d: %w", v, err)
			}
			switch {
			case action.Add != nil:
				if _, ok := active[action.Add.Path]; !ok {
					order = append(order, action.Add.Path)
				}
				active[action.Add.Path] = struct{}{}
			case action.Remove != nil:
				delete(active, action.Remove.Path)
			}
		}
	}

	files := make([]string, 0, len(active))
	for _, p := range order {
		if _, ok := active[p]; ok {
			files = append(files, p)
		}
	}

	return &deltaSnapshot{exists: true, version: versions[len(versions)-1], activeFiles: files}, nil
}

// readDeltaData downloads and decodes all active data files of a Delta table into
// Arrow record batches. The caller owns the returned batches and must Release them.
func readDeltaData(ctx context.Context, client *adlsutil.DataLakeClient, fileSystem, tableDir string, files []string) ([]arrow.RecordBatch, error) {
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
	return batches, nil
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
