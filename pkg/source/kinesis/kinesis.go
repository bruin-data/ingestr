package kinesis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/crypto/sha3"
	"golang.org/x/time/rate"
)

const (
	maxRecordsPerGet   = 1000
	millisBehindCutoff = 1000
	// Kinesis allows 5 GetRecords calls/sec/shard; use 80%
	getRecordsRPS      = 4.0
	getRecordsBurst    = 2
	maxListShardsPages = 100
)

type kinesisCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

type KinesisSource struct {
	creds   kinesisCredentials
	client  *kinesis.Client
	limiter *rate.Limiter
}

func NewKinesisSource() *KinesisSource {
	return &KinesisSource{}
}

func (s *KinesisSource) Schemes() []string {
	return []string{"kinesis"}
}

func (s *KinesisSource) HandlesIncrementality() bool {
	return true
}

func (s *KinesisSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseKinesisURI(uri)
	if err != nil {
		return err
	}
	s.creds = creds

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(creds.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s.client = kinesis.NewFromConfig(awsCfg)
	s.limiter = rate.NewLimiter(rate.Limit(getRecordsRPS), getRecordsBurst)

	config.Debug("[KINESIS] Connected to region %s", creds.Region)
	return nil
}

func (s *KinesisSource) Close(_ context.Context) error {
	return nil
}

func parseKinesisURI(uri string) (kinesisCredentials, error) {
	if !strings.HasPrefix(uri, "kinesis://") {
		return kinesisCredentials{}, fmt.Errorf("invalid kinesis URI: must start with kinesis://")
	}

	rest := strings.TrimPrefix(uri, "kinesis://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return kinesisCredentials{}, fmt.Errorf("failed to parse kinesis URI query: %w", err)
	}

	creds := kinesisCredentials{
		AccessKeyID:     values.Get("aws_access_key_id"),
		SecretAccessKey: values.Get("aws_secret_access_key"),
		Region:          values.Get("region_name"),
	}

	if creds.AccessKeyID == "" {
		return kinesisCredentials{}, fmt.Errorf("kinesis URI: aws_access_key_id is required")
	}
	if creds.SecretAccessKey == "" {
		return kinesisCredentials{}, fmt.Errorf("kinesis URI: aws_secret_access_key is required")
	}
	if creds.Region == "" {
		return kinesisCredentials{}, fmt.Errorf("kinesis URI: region_name is required")
	}

	return creds, nil
}

func (s *KinesisSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("kinesis: stream name is required (use --source-table)")
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"kinesis_msg_id"},
		TableIncrementalKey: "kinesis",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("kinesis source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *KinesisSource) read(ctx context.Context, streamName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if err := s.readStream(ctx, streamName, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *KinesisSource) readStream(ctx context.Context, streamName string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KINESIS] Reading stream %s", streamName)

	shards, err := s.listShards(ctx, streamName)
	if err != nil {
		return fmt.Errorf("failed to list shards: %w", err)
	}
	config.Debug("[KINESIS] Found %d shards", len(shards))

	resolvedStreamName := streamName
	if strings.HasPrefix(streamName, "arn:") {
		parts := strings.Split(streamName, "/")
		if len(parts) > 1 {
			resolvedStreamName = parts[len(parts)-1]
		}
	}

	for _, shardID := range shards {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.readShard(ctx, streamName, resolvedStreamName, shardID, opts, results); err != nil {
			return fmt.Errorf("failed to read shard %s: %w", shardID, err)
		}
	}

	return nil
}

func (s *KinesisSource) readShard(ctx context.Context, streamName, resolvedName, shardID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KINESIS] Reading shard %s", shardID)

	iteratorType := types.ShardIteratorTypeTrimHorizon
	var timestamp *time.Time
	if opts.IntervalStart != nil {
		if opts.IntervalStart.Unix() == 0 {
			iteratorType = types.ShardIteratorTypeLatest
		} else {
			iteratorType = types.ShardIteratorTypeAtTimestamp
			timestamp = opts.IntervalStart
		}
	}

	iterator, err := s.getShardIterator(ctx, streamName, shardID, iteratorType, timestamp)
	if err != nil {
		return err
	}

	for iterator != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.limiter.Wait(ctx); err != nil {
			return err
		}

		resp, err := s.client.GetRecords(ctx, &kinesis.GetRecordsInput{
			ShardIterator: aws.String(iterator),
			Limit:         aws.Int32(maxRecordsPerGet),
		})
		if err != nil {
			return fmt.Errorf("failed to get records: %w", err)
		}

		if len(resp.Records) > 0 {
			items := make([]map[string]interface{}, 0, len(resp.Records))
			for _, record := range resp.Records {
				item := buildRecordItem(record, shardID, resolvedName)
				items = append(items, item)
			}

			batch, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert kinesis records to arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: batch}
			config.Debug("[KINESIS] Sent batch of %d records from shard %s", len(items), shardID)
		}

		if resp.ChildShards != nil {
			for _, childShard := range resp.ChildShards {
				if childShard.ShardId != nil && *childShard.ShardId != "" {
					childID := *childShard.ShardId
					config.Debug("[KINESIS] Discovered child shard %s", childID)
					if err := s.readShard(ctx, streamName, resolvedName, childID, opts, results); err != nil {
						return fmt.Errorf("failed to read child shard %s: %w", childID, err)
					}
				}
			}
		}

		if resp.MillisBehindLatest == nil || *resp.MillisBehindLatest < millisBehindCutoff {
			config.Debug("[KINESIS] Shard %s is caught up", shardID)
			break
		}

		if resp.NextShardIterator == nil {
			break
		}
		iterator = *resp.NextShardIterator
	}

	return nil
}

func buildRecordItem(record types.Record, shardID, streamName string) map[string]interface{} {
	msgID := digest128(shardID + *record.SequenceNumber)

	usec := int64(float64(record.ApproximateArrivalTimestamp.UnixMicro()))
	ts := time.UnixMicro(usec).UTC().Format("2006-01-02T15:04:05.000000+00:00")

	metadata := map[string]interface{}{
		"shard_id":    shardID,
		"seq_no":      *record.SequenceNumber,
		"ts":          ts,
		"partition":   *record.PartitionKey,
		"stream_name": streamName,
	}

	item := map[string]interface{}{
		"kinesis_msg_id": msgID,
		"kinesis":        metadata,
	}

	decoder := json.NewDecoder(bytes.NewReader(record.Data))
	decoder.UseNumber()
	var parsed map[string]interface{}
	if err := decoder.Decode(&parsed); err == nil {
		for k, v := range parsed {
			if k == "kinesis_msg_id" || k == "kinesis" {
				continue
			}
			item[k] = v
		}
	} else {
		item["data"] = string(record.Data)
	}

	return item
}

// digest128 matches ingestr: base64-encoded SHAKE128 digest (15 bytes → 20 chars).
func digest128(v string) string {
	h := make([]byte, 15)
	sha3.ShakeSum128(h, []byte(v))
	return base64.StdEncoding.EncodeToString(h)
}

func (s *KinesisSource) listShards(ctx context.Context, streamName string) ([]string, error) {
	var shardIDs []string
	var nextToken *string

	for page := 0; page < maxListShardsPages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		input := &kinesis.ListShardsInput{}
		if nextToken != nil {
			input.NextToken = nextToken
		} else if strings.HasPrefix(streamName, "arn:") {
			input.StreamARN = aws.String(streamName)
		} else {
			input.StreamName = aws.String(streamName)
		}

		resp, err := s.client.ListShards(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list shards: %w", err)
		}

		for _, shard := range resp.Shards {
			shardIDs = append(shardIDs, *shard.ShardId)
		}

		if resp.NextToken == nil {
			break
		}
		nextToken = resp.NextToken
	}

	return shardIDs, nil
}

func (s *KinesisSource) getShardIterator(ctx context.Context, streamName, shardID string, iteratorType types.ShardIteratorType, timestamp *time.Time) (string, error) {
	input := &kinesis.GetShardIteratorInput{
		ShardId:           aws.String(shardID),
		ShardIteratorType: iteratorType,
	}
	if strings.HasPrefix(streamName, "arn:") {
		input.StreamARN = aws.String(streamName)
	} else {
		input.StreamName = aws.String(streamName)
	}
	if timestamp != nil && iteratorType == types.ShardIteratorTypeAtTimestamp {
		input.Timestamp = timestamp
	}

	resp, err := s.client.GetShardIterator(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to get shard iterator: %w", err)
	}

	if resp.ShardIterator == nil {
		return "", nil
	}
	return *resp.ShardIterator, nil
}
