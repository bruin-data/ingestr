package server

import "strconv"

type RunJobRequest struct {
	SourceCredentialID string `json:"sourceCredentialId"`
	DestCredentialID   string `json:"destCredentialId"`
	SourceURI          string `json:"sourceUri"`
	DestURI            string `json:"destUri"`
	SourceTable        string `json:"sourceTable"`
	DestTable          string `json:"destTable"`

	IncrementalStrategy string   `json:"incrementalStrategy"`
	Strategy            string   `json:"strategy"`
	IncrementalKey      string   `json:"incrementalKey"`
	IntervalStart       string   `json:"intervalStart"`
	IntervalEnd         string   `json:"intervalEnd"`
	PrimaryKeys         []string `json:"primaryKeys"`

	PartitionBy string `json:"partitionBy"`
	ClusterBy   string `json:"clusterBy"`

	FullRefresh      bool   `json:"fullRefresh"`
	SchemaContract   string `json:"schemaContract"`
	SchemaNaming     string `json:"schemaNaming"`
	Progress         string `json:"progress"`
	PageSize         int    `json:"pageSize"`
	LoaderFileSize   int    `json:"loaderFileSize"`
	LoaderFileFormat string `json:"loaderFileFormat"`

	ExtractParallelism int      `json:"extractParallelism"`
	SQLLimit           int      `json:"sqlLimit"`
	SQLExcludeColumns  []string `json:"sqlExcludeColumns"`
	SQLBackend         []string `json:"sqlBackend"`
	Columns            string   `json:"columns"`
	NoInference        bool     `json:"noInference"`
	Mask               []string `json:"mask"`
	TrimWhitespace     bool     `json:"trimWhitespace"`
	NoLoadTimestamp    bool     `json:"noLoadTimestamp"`

	PipelinesDir     string `json:"pipelinesDir"`
	StagingBucket    string `json:"stagingBucket"`
	StagingDataset   string `json:"stagingDataset"`
	Debug            bool   `json:"debug"`
	Stream           bool   `json:"stream"`
	FlushInterval    string `json:"flushInterval"`
	FlushRecords     int    `json:"flushRecords"`
	QueryAnnotations string `json:"queryAnnotations"`
}

func (r RunJobRequest) IngestArgs() []string {
	args := []string{}
	appendString := func(name, value string) {
		if value != "" {
			args = append(args, "--"+name+"="+value)
		}
	}
	appendInt := func(name string, value int) {
		if value > 0 {
			args = append(args, "--"+name+"="+strconv.Itoa(value))
		}
	}
	appendBool := func(name string, value bool) {
		if value {
			args = append(args, "--"+name)
		}
	}
	appendStringSlice := func(name string, values []string) {
		for _, value := range values {
			if value != "" {
				args = append(args, "--"+name+"="+value)
			}
		}
	}

	appendString("source-uri", r.SourceURI)
	appendString("dest-uri", r.DestURI)
	appendString("source-table", r.SourceTable)
	appendString("dest-table", r.DestTable)
	appendString("incremental-key", r.IncrementalKey)
	appendString("incremental-strategy", r.IncrementalStrategy)
	appendString("interval-start", r.IntervalStart)
	appendString("interval-end", r.IntervalEnd)
	appendStringSlice("primary-key", r.PrimaryKeys)
	appendString("partition-by", r.PartitionBy)
	appendString("cluster-by", r.ClusterBy)
	appendBool("full-refresh", r.FullRefresh)
	appendString("schema-contract", r.SchemaContract)
	appendString("schema-naming", r.SchemaNaming)
	appendString("progress", r.Progress)
	appendInt("page-size", r.PageSize)
	appendInt("loader-file-size", r.LoaderFileSize)
	appendString("loader-file-format", r.LoaderFileFormat)
	appendInt("extract-parallelism", r.ExtractParallelism)
	appendInt("sql-limit", r.SQLLimit)
	appendStringSlice("sql-exclude-columns", r.SQLExcludeColumns)
	appendStringSlice("sql-backend", r.SQLBackend)
	appendString("columns", r.Columns)
	appendBool("no-inference", r.NoInference)
	appendStringSlice("mask", r.Mask)
	appendBool("trim-whitespace", r.TrimWhitespace)
	appendBool("no-load-timestamp", r.NoLoadTimestamp)
	appendString("pipelines-dir", r.PipelinesDir)
	appendString("staging-bucket", r.StagingBucket)
	appendString("staging-dataset", r.StagingDataset)
	appendBool("debug", r.Debug)
	appendBool("stream", r.Stream)
	appendString("flush-interval", r.FlushInterval)
	appendInt("flush-records", r.FlushRecords)
	appendString("query-annotations", r.QueryAnnotations)

	args = append(args, "--yes")

	return args
}
