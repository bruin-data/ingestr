package jsonl

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/archiveutil"
)

type JSONLSource struct {
	filePath             string
	archiveMemberPattern string
	archiveLimits        archiveutil.Limits
}

func NewJSONLSource() *JSONLSource {
	return &JSONLSource{}
}

func (s *JSONLSource) Schemes() []string {
	return []string{"jsonl", "ndjson"}
}

func (s *JSONLSource) Connect(ctx context.Context, uri string) error {
	filePath := extractFilePath(uri)
	if filePath == "" {
		return fmt.Errorf("invalid JSONL URI: %s", uri)
	}
	archiveLimits, err := archiveutil.ParseLimitsFromURI(uri)
	if err != nil {
		return err
	}

	filePath, archiveMemberPattern, _ := archiveutil.SplitPath(filePath)
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access JSONL file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}
	if archiveMemberPattern != "" {
		if err := archiveutil.ValidateArchiveSize(info.Size(), archiveLimits); err != nil {
			return err
		}
	}

	s.filePath = filePath
	s.archiveMemberPattern = archiveMemberPattern
	s.archiveLimits = archiveLimits
	config.Debug("[JSONL] Connected to file: %s", filePath)
	return nil
}

func extractFilePath(uri string) string {
	for _, prefix := range []string{"jsonl://", "jsonl:", "ndjson://", "ndjson:"} {
		if strings.HasPrefix(uri, prefix) {
			filePath := strings.TrimPrefix(uri, prefix)
			filePath = strings.TrimPrefix(filePath, "//")
			if queryStart := strings.IndexByte(filePath, '?'); queryStart >= 0 {
				filePath = filePath[:queryStart]
			}
			if decoded, err := url.PathUnescape(filePath); err == nil {
				filePath = decoded
			}
			return filePath
		}
	}
	return ""
}

func (s *JSONLSource) Close(ctx context.Context) error {
	return nil
}

func (s *JSONLSource) HandlesIncrementality() bool {
	return false
}

func (s *JSONLSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
			return nil, fmt.Errorf("JSONL does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *JSONLSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[JSONL] Starting read from file: %s", s.filePath)
	if s.archiveMemberPattern != "" {
		return s.readZIP(ctx, opts)
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		file, err := os.Open(s.filePath)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to open JSONL file: %w", err)}
			return
		}
		defer func() { _ = file.Close() }()

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

		batchNum := 0
		totalRows := 0
		items := make([]map[string]interface{}, 0, batchSize)
		var accBytes int64
		lineNum := 0

		flush := func() error {
			if len(items) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert to Arrow: %w", err)
			}

			batchNum++
			totalRows += len(items)
			config.Debug("[JSONL] Batch %d: %d items (total: %d)", batchNum, len(items), totalRows)

			results <- source.RecordBatchResult{Batch: record}
			items = make([]map[string]interface{}, 0, batchSize)
			accBytes = 0
			return nil
		}

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lineNum++

			if line == "" {
				continue
			}

			var item map[string]interface{}
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)}
				return
			}

			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(item)
				if len(items) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						results <- source.RecordBatchResult{Err: err}
						return
					}
				}
				accBytes += rowBytes
			}
			items = append(items, item)

			if len(items) >= batchSize {
				if err := flush(); err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
			}

			if opts.Limit > 0 && totalRows+len(items) >= opts.Limit {
				items = items[:opts.Limit-totalRows]
				break
			}
		}

		if err := scanner.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("error reading JSONL file: %w", err)}
			return
		}

		if err := flush(); err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		config.Debug("[JSONL] Total: %d items in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *JSONLSource) readZIP(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	archive, err := zip.OpenReader(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP archive: %w", err)
	}
	members, err := archiveutil.SelectZIPMembers(&archive.Reader, s.archiveMemberPattern, s.archiveLimits)
	if err != nil {
		_ = archive.Close()
		return nil, err
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		defer func() { _ = archive.Close() }()

		totalRows := 0
		for _, member := range members {
			if opts.Limit > 0 && totalRows >= opts.Limit {
				return
			}

			metadata, err := archiveutil.Metadata(s.filePath, member)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			spooled, err := archiveutil.SpoolMember(ctx, member)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			spooledPath := spooled.Name()
			_ = spooled.Close()

			memberOpts := opts
			if opts.Limit > 0 {
				memberOpts.Limit = opts.Limit - totalRows
			}
			memberSource := &JSONLSource{filePath: spooledPath}
			memberResults, readErr := memberSource.read(ctx, memberOpts)
			if readErr == nil {
				rows, forwardErr := archiveutil.ForwardBatches(ctx, results, memberResults, metadata, opts.ExcludeColumns, memberOpts.Limit)
				totalRows += rows
				readErr = forwardErr
			}
			_ = os.Remove(spooledPath)
			if readErr != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read ZIP member %q: %w", member.Name, readErr)}
				return
			}
		}
	}()

	return results, nil
}

var _ source.Source = (*JSONLSource)(nil)
