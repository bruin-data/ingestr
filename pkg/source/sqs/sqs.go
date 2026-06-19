package sqs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBatchSize         = 3000
	defaultBatchTimeout      = 5 * time.Second
	defaultWaitTimeSeconds   = int32(2)
	defaultVisibilitySeconds = int32(300)
	maxReceiveMessages       = int32(10)
	maxDeleteMessages        = 10
	maxChangeVisibility      = 10
	streamOrderColumn        = "_ingestr_order"
)

type sqsConfig struct {
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Region            string
	EndpointURL       string
	WaitTimeSeconds   int32
	VisibilitySeconds int32
}

type SQSSource struct {
	cfg    sqsConfig
	client *awssqs.Client

	mu             sync.Mutex
	streamQueueURL string
	nextSeq        int64
	pending        map[int64]string
}

type sqsCommitToken struct {
	MaxSeq int64
}

type sqsReceipt struct {
	seq    int64
	handle string
}

func NewSQSSource() *SQSSource {
	return &SQSSource{
		pending: make(map[int64]string),
	}
}

func (s *SQSSource) Schemes() []string {
	return []string{"sqs"}
}

func (s *SQSSource) HandlesIncrementality() bool {
	return false
}

func (s *SQSSource) SupportsStreaming() bool {
	return true
}

func (s *SQSSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *SQSSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseSQSURI(uri)
	if err != nil {
		return err
	}
	s.cfg = cfg

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" || cfg.SessionToken != "" {
		if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
			return fmt.Errorf("sqs URI: both access_key_id and secret_access_key are required when static credentials are used")
		}
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	if awsCfg.Region == "" {
		return fmt.Errorf("sqs URI: region is required")
	}

	s.client = awssqs.NewFromConfig(awsCfg, func(o *awssqs.Options) {
		if cfg.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
	})

	config.Debug("[SQS] Connected to region %s", awsCfg.Region)
	return nil
}

func (s *SQSSource) Close(_ context.Context) error {
	return nil
}

func (s *SQSSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("sqs source requires a queue name or URL as source-table")
	}

	queueURL, err := s.resolveQueueURL(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	tableName := queueTableName(req.Name)
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	if req.Streaming {
		streamStrategy := req.Strategy
		if streamStrategy == "" {
			streamStrategy = s.DefaultStreamingStrategy()
		}
		primaryKeys := req.PrimaryKeys
		if len(primaryKeys) == 0 {
			primaryKeys = []string{"msg_id"}
		}
		return &source.DynamicSourceTable{
			TableName:           tableName,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(tableName), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, tableName, queueURL, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("SQS source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, queueURL, opts)
		},
	}, nil
}

func streamingEnvelopeSchema(queue string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: queue,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func parseSQSURI(raw string) (sqsConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return sqsConfig{}, fmt.Errorf("invalid sqs URI: %w", err)
	}
	if u.Scheme != "sqs" {
		return sqsConfig{}, fmt.Errorf("invalid sqs URI: must start with sqs://")
	}

	q := u.Query()
	cfg := sqsConfig{
		AccessKeyID:       firstQuery(q, "access_key_id", "aws_access_key_id"),
		SecretAccessKey:   firstQuery(q, "secret_access_key", "aws_secret_access_key"),
		SessionToken:      firstQuery(q, "session_token", "aws_session_token"),
		Region:            firstQuery(q, "region", "region_name", "aws_region"),
		EndpointURL:       firstQuery(q, "endpoint_url", "endpoint"),
		WaitTimeSeconds:   defaultWaitTimeSeconds,
		VisibilitySeconds: defaultVisibilitySeconds,
	}
	if cfg.EndpointURL == "" && u.Host != "" {
		cfg.EndpointURL = endpointFromHost(u.Host)
	}
	if v := firstQuery(q, "wait_time_seconds", "wait_seconds"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 32)
		if err != nil || parsed < 0 || parsed > 20 {
			return sqsConfig{}, fmt.Errorf("sqs URI: wait_time_seconds must be between 0 and 20")
		}
		cfg.WaitTimeSeconds = int32(parsed)
	}
	if v := firstQuery(q, "visibility_timeout", "visibility_timeout_seconds"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 32)
		if err != nil || parsed <= 0 {
			return sqsConfig{}, fmt.Errorf("sqs URI: visibility_timeout must be a positive number of seconds")
		}
		cfg.VisibilitySeconds = int32(parsed)
	}
	return cfg, nil
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
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

func (s *SQSSource) resolveQueueURL(ctx context.Context, name string) (string, error) {
	if strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "http://") {
		return name, nil
	}
	out, err := s.client.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err != nil {
		return "", fmt.Errorf("failed to resolve SQS queue %q: %w", name, err)
	}
	return aws.ToString(out.QueueUrl), nil
}

func queueTableName(name string) string {
	if u, err := url.Parse(name); err == nil && u.Host != "" && u.Path != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		return parts[len(parts)-1]
	}
	return name
}

func (s *SQSSource) read(ctx context.Context, tableName, queueURL string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	streaming := opts.Streaming
	if streaming {
		s.mu.Lock()
		s.streamQueueURL = queueURL
		s.nextSeq = 0
		s.pending = make(map[int64]string)
		s.mu.Unlock()
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		var visibilityErrs <-chan error
		var stopVisibility context.CancelFunc
		if streaming {
			visibilityCtx, cancel := context.WithCancel(ctx)
			stopVisibility = cancel
			errs := make(chan error, 1)
			visibilityErrs = errs
			go s.extendVisibilityLoop(visibilityCtx, queueURL, errs)
		}
		defer func() {
			if stopVisibility != nil {
				stopVisibility()
			}
		}()

		var envelopeCols []schema.Column
		if streaming {
			envelopeCols = streamingEnvelopeSchema(tableName).Columns
		}

		batch := make([]map[string]any, 0, batchSize)
		batchReceipts := make([]sqsReceipt, 0, batchSize)
		totalRead := int64(0)
		limit := int64(opts.Limit)
		lastFlush := time.Now()

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, envelopeCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert SQS messages to Arrow: %w", err)}
				return false
			}
			res := source.RecordBatchResult{Batch: record}
			if streaming {
				res.CommitToken = sqsCommitToken{MaxSeq: batchReceipts[len(batchReceipts)-1].seq}
			}
			results <- res

			if !streaming {
				if _, err := s.deleteReceipts(ctx, queueURL, batchReceipts); err != nil {
					results <- source.RecordBatchResult{Err: err}
					return false
				}
			}

			batch = batch[:0]
			batchReceipts = batchReceipts[:0]
			lastFlush = time.Now()
			return true
		}

		for {
			if ctx.Err() != nil {
				flush()
				return
			}
			select {
			case err := <-visibilityErrs:
				results <- source.RecordBatchResult{Err: err}
				return
			default:
			}

			maxMessages := maxReceiveMessages
			if remaining := batchSize - len(batch); remaining > 0 && remaining < int(maxMessages) {
				maxMessages = int32(remaining)
			}

			out, err := s.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
				QueueUrl:              aws.String(queueURL),
				MaxNumberOfMessages:   maxMessages,
				WaitTimeSeconds:       s.cfg.WaitTimeSeconds,
				VisibilityTimeout:     s.visibilitySeconds(),
				AttributeNames:        []types.QueueAttributeName{types.QueueAttributeNameAll},
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to receive SQS messages: %w", err)}
				return
			}

			if len(out.Messages) == 0 {
				if !flush() {
					return
				}
				if !streaming {
					return
				}
				continue
			}

			receivedReceipts := make([]sqsReceipt, 0, len(out.Messages))
			for _, msg := range out.Messages {
				receivedReceipts = append(receivedReceipts, sqsReceipt{handle: aws.ToString(msg.ReceiptHandle)})
			}
			if streaming {
				if err := s.extendVisibility(ctx, queueURL, receivedReceipts); err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
			}

			for _, msg := range out.Messages {
				receipt := aws.ToString(msg.ReceiptHandle)
				var seq int64
				if streaming {
					seq = s.trackReceipt(receipt)
					batch = append(batch, messageToEnvelope(msg, seq))
				} else {
					batch = append(batch, messageToItem(msg))
				}
				batchReceipts = append(batchReceipts, sqsReceipt{seq: seq, handle: receipt})
				totalRead++

				if limit > 0 && totalRead >= limit {
					flush()
					return
				}
				if len(batch) >= batchSize {
					if !flush() {
						return
					}
				}
			}

			if len(batch) > 0 && time.Since(lastFlush) >= defaultBatchTimeout {
				if !flush() {
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *SQSSource) trackReceipt(handle string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq++
	s.pending[s.nextSeq] = handle
	return s.nextSeq
}

func (s *SQSSource) extendVisibilityLoop(ctx context.Context, queueURL string, errs chan<- error) {
	ticker := time.NewTicker(s.visibilityExtensionInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			receipts, activeQueueURL := s.pendingSnapshot()
			if len(receipts) == 0 {
				continue
			}
			if activeQueueURL != "" {
				queueURL = activeQueueURL
			}
			if queueURL == "" {
				continue
			}
			if err := s.extendPendingVisibility(ctx, queueURL, receipts); err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}
}

func (s *SQSSource) visibilitySeconds() int32 {
	if s.cfg.VisibilitySeconds > 0 {
		return s.cfg.VisibilitySeconds
	}
	return defaultVisibilitySeconds
}

func (s *SQSSource) visibilityExtensionInterval() time.Duration {
	interval := time.Duration(s.visibilitySeconds()) * time.Second / 2
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func (s *SQSSource) CommitStream(ctx context.Context, token any) error {
	tok, ok := token.(sqsCommitToken)
	if !ok {
		return fmt.Errorf("sqs: unexpected commit token type %T", token)
	}

	receipts, queueURL := s.pendingThrough(tok.MaxSeq)
	if len(receipts) == 0 {
		return nil
	}
	if queueURL == "" {
		config.Debug("[SQS] CommitStream: no active queue URL, skipping delete")
		return nil
	}

	committed, err := s.deleteReceipts(ctx, queueURL, receipts)
	if len(committed) > 0 {
		s.removePending(committed)
	}
	return err
}

func (s *SQSSource) pendingThrough(maxSeq int64) ([]sqsReceipt, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts := make([]sqsReceipt, 0)
	for seq, handle := range s.pending {
		if seq <= maxSeq {
			receipts = append(receipts, sqsReceipt{seq: seq, handle: handle})
		}
	}
	return receipts, s.streamQueueURL
}

func (s *SQSSource) pendingSnapshot() ([]sqsReceipt, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts := make([]sqsReceipt, 0, len(s.pending))
	for seq, handle := range s.pending {
		receipts = append(receipts, sqsReceipt{seq: seq, handle: handle})
	}
	return receipts, s.streamQueueURL
}

func (s *SQSSource) isPending(receipt sqsReceipt) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[receipt.seq] == receipt.handle
}

func (s *SQSSource) removePending(receipts []sqsReceipt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range receipts {
		delete(s.pending, receipt.seq)
	}
}

func (s *SQSSource) extendVisibility(ctx context.Context, queueURL string, receipts []sqsReceipt) error {
	return s.changeVisibility(ctx, queueURL, receipts, false)
}

func (s *SQSSource) extendPendingVisibility(ctx context.Context, queueURL string, receipts []sqsReceipt) error {
	return s.changeVisibility(ctx, queueURL, receipts, true)
}

func (s *SQSSource) changeVisibility(ctx context.Context, queueURL string, receipts []sqsReceipt, pendingOnly bool) error {
	for start := 0; start < len(receipts); start += maxChangeVisibility {
		end := start + maxChangeVisibility
		if end > len(receipts) {
			end = len(receipts)
		}
		chunk := receipts[start:end]
		entries := make([]types.ChangeMessageVisibilityBatchRequestEntry, 0, len(chunk))
		byID := make(map[string]sqsReceipt, len(chunk))
		for i, receipt := range chunk {
			id := fmt.Sprintf("m%d", i)
			entries = append(entries, types.ChangeMessageVisibilityBatchRequestEntry{
				Id:                aws.String(id),
				ReceiptHandle:     aws.String(receipt.handle),
				VisibilityTimeout: s.visibilitySeconds(),
			})
			byID[id] = receipt
		}

		out, err := s.client.ChangeMessageVisibilityBatch(ctx, &awssqs.ChangeMessageVisibilityBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  entries,
		})
		if err != nil {
			return fmt.Errorf("failed to extend SQS visibility timeout: %w", err)
		}
		failed := make(map[string]string, len(out.Failed))
		for _, failure := range out.Failed {
			id := aws.ToString(failure.Id)
			receipt, ok := byID[id]
			if !ok || (pendingOnly && !s.isPending(receipt)) {
				continue
			}
			message := aws.ToString(failure.Message)
			if message == "" {
				message = aws.ToString(failure.Code)
			}
			failed[id] = message
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed to extend visibility timeout for %d SQS messages: %v", len(failed), failed)
		}
	}
	return nil
}

func (s *SQSSource) deleteReceipts(ctx context.Context, queueURL string, receipts []sqsReceipt) ([]sqsReceipt, error) {
	committed := make([]sqsReceipt, 0, len(receipts))
	for start := 0; start < len(receipts); start += maxDeleteMessages {
		end := start + maxDeleteMessages
		if end > len(receipts) {
			end = len(receipts)
		}
		chunk := receipts[start:end]
		entries := make([]types.DeleteMessageBatchRequestEntry, 0, len(chunk))
		byID := make(map[string]sqsReceipt, len(chunk))
		for i, receipt := range chunk {
			id := fmt.Sprintf("m%d", i)
			entries = append(entries, types.DeleteMessageBatchRequestEntry{
				Id:            aws.String(id),
				ReceiptHandle: aws.String(receipt.handle),
			})
			byID[id] = receipt
		}

		out, err := s.client.DeleteMessageBatch(ctx, &awssqs.DeleteMessageBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  entries,
		})
		if err != nil {
			return committed, fmt.Errorf("failed to delete SQS messages: %w", err)
		}
		failed := make(map[string]string, len(out.Failed))
		for _, failure := range out.Failed {
			failed[aws.ToString(failure.Id)] = aws.ToString(failure.Message)
		}
		for id, receipt := range byID {
			if _, ok := failed[id]; !ok {
				committed = append(committed, receipt)
			}
		}
		if len(failed) > 0 {
			return committed, fmt.Errorf("failed to delete %d SQS messages: %v", len(failed), failed)
		}
	}
	return committed, nil
}

func messageToItem(msg types.Message) map[string]any {
	body := aws.ToString(msg.Body)
	var data any
	decoder := json.NewDecoder(bytes.NewReader([]byte(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		data = body
	}

	msgID := aws.ToString(msg.MessageId)
	if msgID == "" {
		msgID = digest128(body)
	}

	return map[string]any{
		"data": data,
		"metadata": map[string]any{
			"message_id":         msgID,
			"md5_of_body":        aws.ToString(msg.MD5OfBody),
			"attributes":         msg.Attributes,
			"message_attributes": messageAttributes(msg.MessageAttributes),
		},
		"msg_id": msgID,
	}
}

func messageToEnvelope(msg types.Message, seq int64) map[string]any {
	item := messageToItem(msg)
	msgID, _ := item["msg_id"].(string)
	delete(item, "msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = jsonStringFallback(aws.ToString(msg.Body))
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: seq,
	}
}

func messageAttributes(attrs map[string]types.MessageAttributeValue) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for k, attr := range attrs {
		value := map[string]any{
			"data_type": aws.ToString(attr.DataType),
		}
		if attr.StringValue != nil {
			value["string_value"] = aws.ToString(attr.StringValue)
		}
		if attr.BinaryValue != nil {
			value["binary_value"] = base64.StdEncoding.EncodeToString(attr.BinaryValue)
		}
		if len(attr.StringListValues) > 0 {
			value["string_list_values"] = attr.StringListValues
		}
		if len(attr.BinaryListValues) > 0 {
			encoded := make([]string, 0, len(attr.BinaryListValues))
			for _, v := range attr.BinaryListValues {
				encoded = append(encoded, base64.StdEncoding.EncodeToString(v))
			}
			value["binary_list_values"] = encoded
		}
		out[k] = value
	}
	return out
}

func jsonStringFallback(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return encoded
}

func digest128(v string) string {
	sum := sha256.Sum256([]byte(v))
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

var (
	_ source.Source          = (*SQSSource)(nil)
	_ source.StreamingSource = (*SQSSource)(nil)
	_ source.StreamCommitter = (*SQSSource)(nil)
)
