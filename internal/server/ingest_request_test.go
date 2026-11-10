package server

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRunJobRequestIngestArgs(t *testing.T) {
	req := RunJobRequest{
		SourceURI:                "postgres://user:pass@localhost/db",
		DestURI:                  "duckdb:///tmp/out.db",
		SourceTable:              "public.users",
		DestTable:                "raw.users",
		IncrementalStrategy:      "merge",
		IncrementalKey:           "updated_at",
		IncrementalPredicate:     "target.updated_at >= '2026-01-01'",
		IntervalStart:            "2026-01-01",
		IntervalEnd:              "2026-01-02",
		PrimaryKeys:              []string{"id", "tenant_id"},
		PartitionBy:              "created_at",
		ClusterBy:                "tenant_id,region",
		FullRefresh:              true,
		SchemaContract:           "freeze",
		SchemaNaming:             "snake_case",
		Progress:                 "log",
		PageSize:                 1000,
		LoaderFileSize:           2000,
		LoaderFileFormat:         "parquet",
		ExtractParallelism:       3,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: "7d",
		SQLLimit:                 10,
		SQLExcludeColumns:        []string{"password", "token"},
		SQLBackend:               []string{"adbc"},
		Columns:                  "id:bigint,email:string",
		NoInference:              true,
		Mask:                     []string{"email:email", "phone:phone"},
		TrimWhitespace:           true,
		NoLoadTimestamp:          true,
		PipelinesDir:             ".ingestr",
		StagingBucket:            "s3://bucket/path",
		StagingDataset:           "staging",
		Debug:                    true,
		Stream:                   true,
		FlushInterval:            "15s",
		FlushRecords:             500,
		QueryAnnotations:         `{"pipeline":"daily"}`,
	}

	got := req.IngestArgs()
	want := []string{
		"--source-uri=postgres://user:pass@localhost/db",
		"--dest-uri=duckdb:///tmp/out.db",
		"--source-table=public.users",
		"--dest-table=raw.users",
		"--incremental-key=updated_at",
		"--incremental-predicate=target.updated_at >= '2026-01-01'",
		"--incremental-strategy=merge",
		"--interval-start=2026-01-01",
		"--interval-end=2026-01-02",
		"--primary-key=id",
		"--primary-key=tenant_id",
		"--partition-by=created_at",
		"--cluster-by=tenant_id,region",
		"--full-refresh",
		"--schema-contract=freeze",
		"--schema-naming=snake_case",
		"--progress=log",
		"--page-size=1000",
		"--loader-file-size=2000",
		"--loader-file-format=parquet",
		"--extract-parallelism=3",
		"--extract-partition-by=created_at",
		"--extract-partition-interval=7d",
		"--sql-limit=10",
		"--sql-exclude-columns=password",
		"--sql-exclude-columns=token",
		"--sql-backend=adbc",
		"--columns=id:bigint,email:string",
		"--no-inference",
		"--mask=email:email",
		"--mask=phone:phone",
		"--trim-whitespace",
		"--no-load-timestamp",
		"--pipelines-dir=.ingestr",
		"--staging-bucket=s3://bucket/path",
		"--staging-dataset=staging",
		"--debug",
		"--stream",
		"--flush-interval=15s",
		"--flush-records=500",
		`--query-annotations={"pipeline":"daily"}`,
		"--yes",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRunJobRequestDestinationParallelism(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "independent parallelism",
			body: `{"extractParallelism":2,"destinationParallelism":8}`,
			want: []string{"--extract-parallelism=2", "--destination-parallelism=8", "--yes"},
		},
		{
			name: "omitted destination parallelism",
			body: `{"extractParallelism":2}`,
			want: []string{"--extract-parallelism=2", "--yes"},
		},
		{
			name: "explicit zero reaches CLI validation",
			body: `{"destinationParallelism":0}`,
			want: []string{"--destination-parallelism=0", "--yes"},
		},
		{
			name: "negative parallelism reaches CLI validation",
			body: `{"destinationParallelism":-1}`,
			want: []string{"--destination-parallelism=-1", "--yes"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var req RunJobRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatal(err)
			}
			if got := req.IngestArgs(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("args mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}
