package strategy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/bruin-data/ingestr/pkg/destination"
	csvdest "github.com/bruin-data/ingestr/pkg/destination/csv"
	parquetdest "github.com/bruin-data/ingestr/pkg/destination/parquet"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestFileReplaceLifecycle(t *testing.T) {
	for _, format := range []string{"csv", "parquet"} {
		for _, scenario := range []string{"empty", "success", "source-error", "cancelled"} {
			t.Run(format+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				path := filepath.Join(t.TempDir(), "output."+format)
				old := []byte("previous output must survive failure")
				require.NoError(t, os.WriteFile(path, old, 0o644))
				require.NoError(t, os.Chmod(path, 0o640))
				var dest destination.Destination = csvdest.NewCSVDestination()
				if format == "parquet" {
					dest = parquetdest.NewParquetDestination()
				}
				require.NoError(t, dest.Connect(ctx, format+"://"+path))
				job, _, _ := minimalJob()
				job.Destination = dest
				job.Schema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
				var results []source.RecordBatchResult
				if scenario != "empty" {
					results = append(results, source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{17, 29}, nil)})
				}
				sourceErr := errors.New("source failed after a batch")
				if scenario == "source-error" {
					results = append(results, source.RecordBatchResult{Err: sourceErr})
				}
				if scenario == "cancelled" {
					cancel()
				}
				job.BufferedRecords = mustClosedRecords(results...)
				err := (&ReplaceStrategy{}).Execute(ctx, job)
				require.NoError(t, dest.Close(context.Background()))
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				info, statErr := os.Stat(path)
				require.NoError(t, statErr)
				require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
				switch scenario {
				case "source-error", "cancelled":
					if scenario == "source-error" {
						require.ErrorIs(t, err, sourceErr)
					} else {
						require.ErrorIs(t, err, context.Canceled)
					}
					require.Equal(t, old, got)
				default:
					require.NoError(t, err)
					if format == "csv" {
						want := "id\n"
						if scenario == "success" {
							want += "17\n29\n"
						}
						require.Equal(t, want, string(got))
					} else {
						reader, err := file.OpenParquetFile(path, false)
						require.NoError(t, err)
						defer func() { require.NoError(t, reader.Close()) }()
						want := int64(0)
						if scenario == "success" {
							want = 2
						}
						require.Equal(t, want, reader.NumRows())
						require.Equal(t, "id", reader.MetaData().Schema.Column(0).Name())
					}
				}
				entries, err := os.ReadDir(filepath.Dir(path))
				require.NoError(t, err)
				require.Len(t, entries, 1, "temporary output must be removed")
			})
		}
	}
}
