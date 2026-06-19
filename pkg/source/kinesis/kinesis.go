package kinesis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
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
	streamOrderColumn  = "_ingestr_order"
)

type kinesisCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
	EndpointURL     string
}

type KinesisSource struct {
	creds   kinesisCredentials
	client  *kinesis.Client
	limiter *rate.Limiter

	mu         sync.Mutex
	streamSeq  int64
	streamSeen map[string]struct{}
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

func (s *KinesisSource) SupportsStreaming() bool {
	return true
}

func (s *KinesisSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
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
			credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s.client = kinesis.NewFromConfig(awsCfg, func(o *kinesis.Options) {
		if creds.EndpointURL != "" {
			o.BaseEndpoint = aws.String(creds.EndpointURL)
		}
	})
	s.limiter = rate.NewLimiter(rate.Limit(getRecordsRPS), getRecordsBurst)

	config.Debug("[KINESIS] Connected to region %s", creds.Region)
	return nil
}

func (s *KinesisSource) Close(_ context.Context) error {
	return nil
}

func parseKinesisURI(raw string) (kinesisCredentials, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return kinesisCredentials{}, fmt.Errorf("invalid kinesis URI: %w", err)
	}
	if u.Scheme != "kinesis" {
		return kinesisCredentials{}, fmt.Errorf("invalid kinesis URI: must start with kinesis://")
	}

	values := u.Query()
	creds := kinesisCredentials{
		AccessKeyID:     firstQuery(values, "aws_access_key_id", "access_key_id"),
		SecretAccessKey: firstQuery(values, "aws_secret_access_key", "secret_access_key"),
		SessionToken:    firstQuery(values, "aws_session_token", "session_token"),
		Region:          firstQuery(values, "region_name", "region", "aws_region"),
		EndpointURL:     firstQuery(values, "endpoint_url", "endpoint"),
	}
	if creds.EndpointURL == "" && u.Host != "" {
		creds.EndpointURL = endpointFromHost(u.Host)
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

func endpointFromHost(host string) string {
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	scheme := "https"
	if name == "localhost" || name == "127.0.0.1" || name == "0.0.0.0" || strings.HasPrefix(name, "192.168.") || strings.HasPrefix(name, "10.") || isPrivate172(name) {
		scheme = "http"
	}
	return scheme + "://" + host
}

func isPrivate172(host string) bool {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return false
	}
	return ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

func (s *KinesisSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("kinesis: stream name is required (use --source-table)")
	}

	if req.Streaming {
		strategy := req.Strategy
		if strategy == "" {
			strategy = s.DefaultStreamingStrategy()
		}
		primaryKeys := req.PrimaryKeys
		if len(primaryKeys) == 0 {
			primaryKeys = []string{"msg_id"}
		}
		return &source.DynamicSourceTable{
			TableName:           req.Name,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       strategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(req.Name), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, req.Name, opts)
			},
		}, nil
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

func streamingEnvelopeSchema(streamName string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: streamName,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func (s *KinesisSource) read(ctx context.Context, streamName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if err := s.readStream(ctx, streamName, opts, results); err != nil {
			if ctx.Err() != nil {
				return
			}
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

	if opts.Streaming {
		s.mu.Lock()
		s.streamSeq = 0
		s.streamSeen = make(map[string]struct{}, len(shards))
		s.mu.Unlock()
		return s.readShardsConcurrently(ctx, streamName, resolvedStreamName, shards, opts, results)
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

func (s *KinesisSource) readShardsConcurrently(ctx context.Context, streamName, resolvedName string, shards []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readShardsConcurrentlyWith(ctx, shards, opts, func(ctx context.Context, shardID string) error {
		return s.readShard(ctx, streamName, resolvedName, shardID, opts, results)
	})
}

func (s *KinesisSource) readShardsConcurrentlyWith(ctx context.Context, shards []string, opts source.ReadOptions, read func(context.Context, string) error) error {
	if len(shards) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	if opts.Streaming {
		shards = s.claimStreamShards(shards)
		if len(shards) == 0 {
			return nil
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(shards))
	var wg sync.WaitGroup
	for _, shardID := range shards {
		shardID := shardID
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := read(ctx, shardID); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("failed to read shard %s: %w", shardID, err)
				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		select {
		case err := <-errCh:
			return err
		default:
			return ctx.Err()
		}
	case err := <-errCh:
		cancel()
		<-done
		return err
	case <-ctx.Done():
		<-done
		select {
		case err := <-errCh:
			return err
		default:
		}
		return ctx.Err()
	}
}

func (s *KinesisSource) claimStreamShards(shards []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamSeen == nil {
		s.streamSeen = make(map[string]struct{}, len(shards))
	}
	claimed := make([]string, 0, len(shards))
	for _, shardID := range shards {
		if _, ok := s.streamSeen[shardID]; ok {
			config.Debug("[KINESIS] Skipping already scheduled shard %s", shardID)
			continue
		}
		s.streamSeen[shardID] = struct{}{}
		claimed = append(claimed, shardID)
	}
	return claimed
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
				var item map[string]interface{}
				if opts.Streaming {
					item = s.buildRecordEnvelope(record, shardID, resolvedName)
				} else {
					item = buildRecordItem(record, shardID, resolvedName)
				}
				items = append(items, item)
			}

			var cols []schema.Column
			if opts.Streaming {
				cols = streamingEnvelopeSchema(resolvedName).Columns
			}
			batch, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert kinesis records to arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: batch}
			config.Debug("[KINESIS] Sent batch of %d records from shard %s", len(items), shardID)
		}

		if len(resp.ChildShards) > 0 {
			childIDs := childShardIDs(resp.ChildShards)
			for _, childID := range childIDs {
				config.Debug("[KINESIS] Discovered child shard %s", childID)
			}
			if err := s.readChildShards(ctx, streamName, resolvedName, childIDs, opts, results); err != nil {
				return err
			}
		}

		if !opts.Streaming && (resp.MillisBehindLatest == nil || *resp.MillisBehindLatest < millisBehindCutoff) {
			config.Debug("[KINESIS] Shard %s is caught up", shardID)
			break
		}

		if resp.NextShardIterator == nil {
			break
		}
		iterator = *resp.NextShardIterator
		if opts.Streaming && len(resp.Records) == 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	return nil
}

func (s *KinesisSource) readChildShards(ctx context.Context, streamName, resolvedName string, childIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(childIDs) == 0 {
		return nil
	}
	if opts.Streaming {
		return s.readShardsConcurrently(ctx, streamName, resolvedName, childIDs, opts, results)
	}
	for _, childID := range childIDs {
		if err := s.readShard(ctx, streamName, resolvedName, childID, opts, results); err != nil {
			return fmt.Errorf("failed to read child shard %s: %w", childID, err)
		}
	}
	return nil
}

func childShardIDs(childShards []types.ChildShard) []string {
	seen := make(map[string]struct{}, len(childShards))
	ids := make([]string, 0, len(childShards))
	for _, childShard := range childShards {
		childID := aws.ToString(childShard.ShardId)
		if childID == "" {
			continue
		}
		if _, ok := seen[childID]; ok {
			continue
		}
		seen[childID] = struct{}{}
		ids = append(ids, childID)
	}
	return ids
}

func buildRecordItem(record types.Record, shardID, streamName string) map[string]interface{} {
	seqNo := aws.ToString(record.SequenceNumber)
	partitionKey := aws.ToString(record.PartitionKey)
	msgID := digest128(shardID + seqNo)

	tsValue := time.Now().UTC()
	if record.ApproximateArrivalTimestamp != nil {
		tsValue = record.ApproximateArrivalTimestamp.UTC()
	}
	usec := int64(float64(tsValue.UnixMicro()))
	ts := time.UnixMicro(usec).UTC().Format("2006-01-02T15:04:05.000000+00:00")

	metadata := map[string]interface{}{
		"shard_id":    shardID,
		"seq_no":      seqNo,
		"ts":          ts,
		"partition":   partitionKey,
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

func (s *KinesisSource) buildRecordEnvelope(record types.Record, shardID, streamName string) map[string]interface{} {
	item := buildRecordItem(record, shardID, streamName)
	msgID, _ := item["kinesis_msg_id"].(string)
	delete(item, "kinesis_msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = jsonStringFallback(string(record.Data))
	}
	return map[string]interface{}{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: s.nextStreamOrder(),
	}
}

func jsonStringFallback(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return encoded
}

func (s *KinesisSource) nextStreamOrder() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamSeq++
	return s.streamSeq
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

var (
	_ source.Source          = (*KinesisSource)(nil)
	_ source.StreamingSource = (*KinesisSource)(nil)
)
