package bigquery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	storage "cloud.google.com/go/bigquery/storage/apiv1"
	storagepb "cloud.google.com/go/bigquery/storage/apiv1/storagepb"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// StorageWriteArrowClient wraps the BigQuery Storage Write API client for Arrow format
type StorageWriteArrowClient struct {
	client    *storage.BigQueryWriteClient
	projectID string

	appendWorker func(ctx context.Context, tablePath string, records <-chan arrow.RecordBatch, workerID int) error
}

// grpcErrorDetail extracts detailed information from a gRPC error, including
// status details like StorageError. Returns a descriptive string.
func grpcErrorDetail(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return err.Error()
	}
	msg := fmt.Sprintf("code=%s, msg=%s", st.Code(), st.Message())
	details := st.Details()
	if len(details) == 0 {
		// No typed details; dump the raw status proto for anything we might be missing
		msg += fmt.Sprintf(", raw_status=%+v", st.Proto())
		return msg
	}
	for _, detail := range details {
		switch d := detail.(type) {
		case *storagepb.StorageError:
			msg += fmt.Sprintf(", storage_error: code=%s, entity=%s, error_message=%s",
				d.GetCode(), d.GetEntity(), d.GetErrorMessage())
		default:
			msg += fmt.Sprintf(", detail(%T): %+v", d, d)
		}
	}
	return msg
}

// NewStorageWriteArrowClient creates a new Storage Write API client for Arrow format
func NewStorageWriteArrowClient(ctx context.Context, projectID string, opts ...option.ClientOption) (*StorageWriteArrowClient, error) {
	grpcOpts := []option.ClientOption{
		option.WithGRPCDialOption(grpc.WithInitialWindowSize(1024 * 1024 * 128)),     // 128MB window
		option.WithGRPCDialOption(grpc.WithInitialConnWindowSize(1024 * 1024 * 128)), // 128MB connection window
		option.WithGRPCDialOption(grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*100), // 100MB max receive
			grpc.MaxCallSendMsgSize(1024*1024*100), // 100MB max send
			grpc.UseCompressor("gzip"),             // gzip compression
		)),
		option.WithGRPCDialOption(grpc.WithWriteBufferSize(1024 * 1024 * 8)), // 8MB write buffer
		option.WithGRPCDialOption(grpc.WithReadBufferSize(1024 * 1024 * 8)),  // 8MB read buffer
	}
	allOpts := append(grpcOpts, opts...)

	client, err := storage.NewBigQueryWriteClient(ctx, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage write client: %w", err)
	}

	return &StorageWriteArrowClient{
		client:    client,
		projectID: projectID,
	}, nil
}

// Close closes the client
func (c *StorageWriteArrowClient) Close() error {
	return c.client.Close()
}

func (c *StorageWriteArrowClient) runAppendWorker(ctx context.Context, tablePath string, records <-chan arrow.RecordBatch, workerID int) error {
	if c.appendWorker != nil {
		return c.appendWorker(ctx, tablePath, records, workerID)
	}
	return c.appendArrowStreamWorker(ctx, tablePath, records, workerID)
}

// maxAppendRequestBytes is the maximum serialized size for a single AppendRows request.
// BigQuery Storage Write API rejects requests over 10MB with INVALID_ARGUMENT.
// We use 9MB to leave headroom for the proto envelope (schema, write stream path, etc.).
const maxAppendRequestBytes = 9 * 1024 * 1024

var openStreamRetryBackoff = []time.Duration{
	250 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
}

var sendRetryBackoff = []time.Duration{
	250 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
}

// AppendArrowStream appends Arrow records using parallel streams for maximum throughput
func (c *StorageWriteArrowClient) AppendArrowStream(ctx context.Context, tablePath string, records <-chan arrow.RecordBatch, parallelism int) error {
	numStreams := parallelism
	if numStreams <= 0 {
		numStreams = defaultWriteParallelism
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once

	// Create channels to distribute records to workers
	workerChans := make([]chan arrow.RecordBatch, numStreams)
	for i := range workerChans {
		workerChans[i] = make(chan arrow.RecordBatch, 2)
	}

	setFirstErr := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		firstErrOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	// Start worker goroutines, each with its own stream
	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		workerID := i
		workerChan := workerChans[i]
		go func() {
			defer wg.Done()
			setFirstErr(c.runAppendWorker(workerCtx, tablePath, workerChan, workerID))
		}()
	}

	// Distribute records to workers in round-robin fashion.
	// The select on ctx.Done() prevents blocking when a worker has exited
	// (e.g. due to cancellation) and its channel buffer is full.
	workerIdx := 0
distributeLoop:
	for {
		select {
		case <-workerCtx.Done():
			break distributeLoop
		case record, ok := <-records:
			if !ok {
				break distributeLoop
			}
			select {
			case workerChans[workerIdx] <- record:
				workerIdx = (workerIdx + 1) % numStreams
			case <-workerCtx.Done():
				if record != nil {
					record.Release()
				}
				break distributeLoop
			}
		}
	}

	// Close all worker channels
	for _, ch := range workerChans {
		close(ch)
	}

	// Wait for all workers to complete
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// AppendArrowStreamFromSource reads from a RecordBatchResult channel directly,
// avoiding an intermediate goroutine/channel for error extraction.
func (c *StorageWriteArrowClient) AppendArrowStreamFromSource(ctx context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error {
	numStreams := parallelism
	if numStreams <= 0 {
		numStreams = defaultWriteParallelism
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once

	workerChans := make([]chan arrow.RecordBatch, numStreams)
	for i := range workerChans {
		workerChans[i] = make(chan arrow.RecordBatch, 2)
	}

	setFirstErr := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		firstErrOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		workerID := i
		workerChan := workerChans[i]
		go func() {
			defer wg.Done()
			setFirstErr(c.runAppendWorker(workerCtx, tablePath, workerChan, workerID))
		}()
	}

	// Target sub-batch size for even worker distribution.
	// Source batches (default 50k rows) are split into ~10k-row sub-batches
	// so all 16 workers get utilized instead of only ceil(totalBatches/numStreams).
	const targetSubBatchRows = 25000

	var sourceErr error
	workerIdx := 0
	sendToWorker := func(batch arrow.RecordBatch) bool {
		select {
		case workerChans[workerIdx] <- batch:
			workerIdx = (workerIdx + 1) % numStreams
			return true
		case <-workerCtx.Done():
			if batch != nil {
				batch.Release()
			}
			return false
		}
	}

dispatchLoop:
	for {
		select {
		case <-workerCtx.Done():
			break dispatchLoop
		case result, ok := <-records:
			if !ok {
				break dispatchLoop
			}
			if result.Err != nil {
				sourceErr = result.Err
				cancel()
				break dispatchLoop
			}
			if result.Batch == nil {
				continue
			}
			batch := result.Batch
			numRows := batch.NumRows()
			if numRows > int64(targetSubBatchRows) {
				dispatchedAll := true
				for start := int64(0); start < numRows; start += int64(targetSubBatchRows) {
					end := start + int64(targetSubBatchRows)
					if end > numRows {
						end = numRows
					}
					sub := batch.NewSlice(start, end)
					if !sendToWorker(sub) {
						dispatchedAll = false
						break
					}
				}
				batch.Release()
				if !dispatchedAll {
					break dispatchLoop
				}
				continue
			}
			if !sendToWorker(batch) {
				break dispatchLoop
			}
		}
	}

	for _, ch := range workerChans {
		close(ch)
	}

	wg.Wait()

	if firstErr != nil {
		return errors.Join(firstErr, sourceErr)
	}
	if sourceErr != nil {
		return sourceErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// AppendArrowPendingStreamsFromSource writes to application-created PENDING streams
// with explicit offsets, finalizes them, and commits them atomically.
func (c *StorageWriteArrowClient) AppendArrowPendingStreamsFromSource(ctx context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error {
	numStreams := parallelism
	if numStreams <= 0 {
		numStreams = defaultWriteParallelism
	}

	streamNames := make([]string, numStreams)
	for i := 0; i < numStreams; i++ {
		streamName, err := c.createPendingWriteStreamWithRetry(ctx, tablePath, i)
		if err != nil {
			return err
		}
		streamNames[i] = streamName
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once

	workerChans := make([]chan arrow.RecordBatch, numStreams)
	for i := range workerChans {
		workerChans[i] = make(chan arrow.RecordBatch, 2)
	}

	setFirstErr := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		firstErrOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		workerID := i
		streamName := streamNames[i]
		workerChan := workerChans[i]
		go func() {
			defer wg.Done()
			_, err := c.appendPendingStreamWorker(workerCtx, streamName, workerChan, workerID)
			setFirstErr(err)
		}()
	}

	const targetSubBatchRows = 25000

	var sourceErr error
	workerIdx := 0
	sendToWorker := func(batch arrow.RecordBatch) bool {
		select {
		case workerChans[workerIdx] <- batch:
			workerIdx = (workerIdx + 1) % numStreams
			return true
		case <-workerCtx.Done():
			if batch != nil {
				batch.Release()
			}
			return false
		}
	}

dispatchLoop:
	for {
		select {
		case <-workerCtx.Done():
			break dispatchLoop
		case result, ok := <-records:
			if !ok {
				break dispatchLoop
			}
			if result.Err != nil {
				sourceErr = result.Err
				cancel()
				break dispatchLoop
			}
			if result.Batch == nil {
				continue
			}
			batch := result.Batch
			numRows := batch.NumRows()
			if numRows > int64(targetSubBatchRows) {
				dispatchedAll := true
				for start := int64(0); start < numRows; start += int64(targetSubBatchRows) {
					end := start + int64(targetSubBatchRows)
					if end > numRows {
						end = numRows
					}
					sub := batch.NewSlice(start, end)
					if !sendToWorker(sub) {
						dispatchedAll = false
						break
					}
				}
				batch.Release()
				if !dispatchedAll {
					break dispatchLoop
				}
				continue
			}
			if !sendToWorker(batch) {
				break dispatchLoop
			}
		}
	}

	for _, ch := range workerChans {
		close(ch)
	}

	wg.Wait()

	if firstErr != nil {
		return errors.Join(firstErr, sourceErr)
	}
	if sourceErr != nil {
		return sourceErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	resp, err := c.batchCommitPendingStreamsWithRetry(ctx, tablePath, streamNames)
	if err != nil {
		return err
	}
	if len(resp.GetStreamErrors()) > 0 {
		return fmt.Errorf("failed to commit pending streams: %v", resp.GetStreamErrors())
	}
	if resp.GetCommitTime() == nil {
		return errors.New("pending streams committed without commit time")
	}

	return nil
}

func (c *StorageWriteArrowClient) createPendingWriteStreamWithRetry(ctx context.Context, tablePath string, workerID int) (string, error) {
	req := &storagepb.CreateWriteStreamRequest{
		Parent: tablePath,
		WriteStream: &storagepb.WriteStream{
			Type: storagepb.WriteStream_PENDING,
		},
	}

	resp, err := c.client.CreateWriteStream(ctx, req)
	if err == nil {
		return resp.GetName(), nil
	}

	lastErr := err
	for attempt, backoff := range openStreamRetryBackoff {
		if !isRetryableSendError(lastErr) {
			break
		}

		config.Debug(
			"[DEST] Pending worker %d: retryable create stream error, retry %d/%d after %s: %s",
			workerID,
			attempt+1,
			len(openStreamRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		if err := sleepWithContext(ctx, backoff); err != nil {
			return "", err
		}

		resp, err = c.client.CreateWriteStream(ctx, req)
		if err == nil {
			return resp.GetName(), nil
		}
		lastErr = err
	}

	return "", fmt.Errorf("failed to create pending write stream (%s): %w", grpcErrorDetail(lastErr), lastErr)
}

func (c *StorageWriteArrowClient) openAppendRowsWithRetry(ctx context.Context, workerID int) (storagepb.BigQueryWrite_AppendRowsClient, error) {
	stream, err := c.client.AppendRows(ctx)
	if err == nil {
		return stream, nil
	}

	lastErr := err
	for attempt, backoff := range openStreamRetryBackoff {
		if !isRetryableSendError(lastErr) {
			break
		}

		config.Debug(
			"[DEST] Pending worker %d: retryable append stream open error, retry %d/%d after %s: %s",
			workerID,
			attempt+1,
			len(openStreamRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}

		stream, err = c.client.AppendRows(ctx)
		if err == nil {
			return stream, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("failed to open pending append stream (%s): %w", grpcErrorDetail(lastErr), lastErr)
}

func closeAppendRowsStream(stream storagepb.BigQueryWrite_AppendRowsClient) {
	if stream == nil {
		return
	}
	_ = stream.CloseSend()
}

func isAlreadyExistsStatus(err error) bool {
	return status.Code(err) == codes.AlreadyExists
}

func (c *StorageWriteArrowClient) appendPendingSerializedBatch(
	ctx context.Context,
	stream storagepb.BigQueryWrite_AppendRowsClient,
	streamName string,
	schemaBytes []byte,
	data []byte,
	offset int64,
	workerID int,
	streamInitialized bool,
) (storagepb.BigQueryWrite_AppendRowsClient, bool, error) {
	current := stream
	initialized := streamInitialized
	var lastErr error

	for attempt := 0; ; attempt++ {
		if current == nil {
			var err error
			current, err = c.openAppendRowsWithRetry(ctx, workerID)
			if err != nil {
				return nil, false, err
			}
			initialized = false
		}

		req := &storagepb.AppendRowsRequest{
			WriteStream: streamName,
			Offset:      wrapperspb.Int64(offset),
			Rows: &storagepb.AppendRowsRequest_ArrowRows{
				ArrowRows: &storagepb.AppendRowsRequest_ArrowData{
					Rows: &storagepb.ArrowRecordBatch{
						SerializedRecordBatch: data,
					},
				},
			},
		}
		if !initialized {
			req.Rows.(*storagepb.AppendRowsRequest_ArrowRows).ArrowRows.WriterSchema = &storagepb.ArrowSchema{
				SerializedSchema: schemaBytes,
			}
		}

		lastErr = current.Send(req)
		if lastErr == nil {
			resp, err := current.Recv()
			if err == nil {
				if respErr := resp.GetError(); respErr != nil {
					lastErr = status.ErrorProto(respErr)
				} else if len(resp.RowErrors) > 0 {
					closeAppendRowsStream(current)
					return nil, false, fmt.Errorf("row errors: %v", resp.RowErrors)
				} else {
					if appendResult := resp.GetAppendResult(); appendResult != nil && appendResult.GetOffset() != nil {
						if appendResult.GetOffset().GetValue() != offset {
							closeAppendRowsStream(current)
							return nil, false, fmt.Errorf(
								"append result offset mismatch: got %d want %d",
								appendResult.GetOffset().GetValue(),
								offset,
							)
						}
					}
					return current, true, nil
				}
			} else {
				lastErr = err
			}
		}

		if isAlreadyExistsStatus(lastErr) {
			return current, true, nil
		}
		if !isRetryableSendError(lastErr) {
			closeAppendRowsStream(current)
			return nil, false, fmt.Errorf(
				"failed pending append at offset %d (%s): %w",
				offset,
				grpcErrorDetail(lastErr),
				lastErr,
			)
		}
		if attempt >= len(sendRetryBackoff) {
			closeAppendRowsStream(current)
			return nil, false, fmt.Errorf(
				"failed pending append after retries at offset %d (%s): %w",
				offset,
				grpcErrorDetail(lastErr),
				lastErr,
			)
		}

		config.Debug(
			"[DEST] Pending worker %d: retryable append error at offset %d, retry %d/%d after %s: %s",
			workerID,
			offset,
			attempt+1,
			len(sendRetryBackoff),
			sendRetryBackoff[attempt],
			grpcErrorDetail(lastErr),
		)

		closeAppendRowsStream(current)
		current = nil

		if err := sleepWithContext(ctx, sendRetryBackoff[attempt]); err != nil {
			return nil, false, err
		}
	}
}

func (c *StorageWriteArrowClient) appendPendingStreamWorker(
	ctx context.Context,
	streamName string,
	records <-chan arrow.RecordBatch,
	workerID int,
) (int64, error) {
	var stream storagepb.BigQueryWrite_AppendRowsClient
	streamInitialized := false
	nextOffset := int64(0)

	for record := range records {
		if ctx.Err() != nil {
			closeAppendRowsStream(stream)
			if record != nil {
				record.Release()
			}
			return 0, ctx.Err()
		}
		if record == nil {
			continue
		}
		if record.NumRows() == 0 {
			record.Release()
			continue
		}

		schemaBytes, err := serializeArrowSchema(record.Schema())
		if err != nil {
			record.Release()
			closeAppendRowsStream(stream)
			return 0, fmt.Errorf("failed to serialize arrow schema: %w", err)
		}

		data, serErr := serializeArrowRecordBatch(record)
		if serErr != nil {
			record.Release()
			closeAppendRowsStream(stream)
			return 0, fmt.Errorf("failed to serialize record batch: %w", serErr)
		}

		if len(data) <= maxAppendRequestBytes {
			stream, streamInitialized, err = c.appendPendingSerializedBatch(
				ctx,
				stream,
				streamName,
				schemaBytes,
				data,
				nextOffset,
				workerID,
				streamInitialized,
			)
			if err != nil {
				record.Release()
				return 0, err
			}
			nextOffset += record.NumRows()
		} else {
			subBatches := splitRecordBatch(record, maxAppendRequestBytes)
			for _, sub := range subBatches {
				stream, streamInitialized, err = c.appendPendingSerializedBatch(
					ctx,
					stream,
					streamName,
					schemaBytes,
					sub.data,
					nextOffset,
					workerID,
					streamInitialized,
				)
				if err != nil {
					sub.record.Release()
					record.Release()
					return 0, err
				}
				nextOffset += sub.record.NumRows()
				sub.record.Release()
			}
		}

		record.Release()
	}

	closeAppendRowsStream(stream)

	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	finalizeResp, err := c.finalizePendingWriteStreamWithRetry(ctx, streamName, workerID)
	if err != nil {
		return 0, err
	}
	if finalizeResp.GetRowCount() != nextOffset {
		return 0, fmt.Errorf(
			"finalized row count mismatch for %s: got %d want %d",
			streamName,
			finalizeResp.GetRowCount(),
			nextOffset,
		)
	}

	return nextOffset, nil
}

func (c *StorageWriteArrowClient) finalizePendingWriteStreamWithRetry(
	ctx context.Context,
	streamName string,
	workerID int,
) (*storagepb.FinalizeWriteStreamResponse, error) {
	req := &storagepb.FinalizeWriteStreamRequest{Name: streamName}
	resp, err := c.client.FinalizeWriteStream(ctx, req)
	if err == nil {
		return resp, nil
	}

	lastErr := err
	for attempt, backoff := range openStreamRetryBackoff {
		if !isRetryableSendError(lastErr) {
			break
		}

		config.Debug(
			"[DEST] Pending worker %d: retryable finalize error, retry %d/%d after %s: %s",
			workerID,
			attempt+1,
			len(openStreamRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}

		resp, err = c.client.FinalizeWriteStream(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("failed to finalize pending write stream (%s): %w", grpcErrorDetail(lastErr), lastErr)
}

func (c *StorageWriteArrowClient) batchCommitPendingStreamsWithRetry(
	ctx context.Context,
	tablePath string,
	streamNames []string,
) (*storagepb.BatchCommitWriteStreamsResponse, error) {
	req := &storagepb.BatchCommitWriteStreamsRequest{
		Parent:       tablePath,
		WriteStreams: streamNames,
	}

	resp, err := c.client.BatchCommitWriteStreams(ctx, req)
	if err == nil {
		return resp, nil
	}

	lastErr := err
	for attempt, backoff := range openStreamRetryBackoff {
		if !isRetryableSendError(lastErr) {
			break
		}

		config.Debug(
			"[DEST] Retryable batch commit error, retry %d/%d after %s: %s",
			attempt+1,
			len(openStreamRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}

		resp, err = c.client.BatchCommitWriteStreams(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("failed to commit pending write streams (%s): %w", grpcErrorDetail(lastErr), lastErr)
}

// streamState tracks the state of a single BigQuery append stream and its receiver goroutine.
type streamState struct {
	stream   storagepb.BigQueryWrite_AppendRowsClient
	recvErr  chan error
	recvDone chan struct{}
}

// openStream creates a new append stream and starts a receiver goroutine for it.
func (c *StorageWriteArrowClient) openStream(ctx context.Context, _ int) (*streamState, error) {
	stream, err := c.client.AppendRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create append stream: %w", err)
	}

	ss := &streamState{
		stream:   stream,
		recvErr:  make(chan error, 1),
		recvDone: make(chan struct{}),
	}

	go func() {
		defer close(ss.recvDone)
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				select {
				case ss.recvErr <- fmt.Errorf("failed to receive response (%s): %w", grpcErrorDetail(err), err):
				default:
				}
				return
			}
			if respErr := resp.GetError(); respErr != nil {
				select {
				case ss.recvErr <- fmt.Errorf("append error: %v", respErr):
				default:
				}
				return
			}
			if len(resp.RowErrors) > 0 {
				select {
				case ss.recvErr <- fmt.Errorf("row errors: %v", resp.RowErrors):
				default:
				}
				return
			}
		}
	}()

	return ss, nil
}

// closeStream cleanly shuts down a stream and waits for the receiver goroutine to exit.
func closeStream(ss *streamState) {
	if ss == nil || ss.stream == nil {
		return
	}
	_ = ss.stream.CloseSend()
	<-ss.recvDone
}

// isRetryableSendError returns true for transient gRPC errors where reopening
// the stream and resending is likely to succeed.
func isRetryableSendError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Internal, codes.Unavailable, codes.Aborted, codes.ResourceExhausted, codes.DeadlineExceeded:
		return true
	}
	return false
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *StorageWriteArrowClient) openStreamWithRetry(ctx context.Context, workerID int) (*streamState, error) {
	ss, err := c.openStream(ctx, workerID)
	if err == nil {
		return ss, nil
	}

	lastErr := err
	for attempt, backoff := range openStreamRetryBackoff {
		if !isRetryableSendError(lastErr) {
			break
		}

		config.Debug(
			"[DEST] Worker %d: retryable stream open error, retry %d/%d after %s: %s",
			workerID,
			attempt+1,
			len(openStreamRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}

		ss, err = c.openStream(ctx, workerID)
		if err == nil {
			return ss, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

func (c *StorageWriteArrowClient) sendRequestWithRetry(
	ctx context.Context,
	workerID int,
	ss *streamState,
	req *storagepb.AppendRowsRequest,
	recordSchema *arrow.Schema,
) (*streamState, error) {
	current := ss
	send := func(stream *streamState) error {
		return stream.stream.Send(req)
	}

	lastErr := send(current)
	if lastErr == nil {
		return current, nil
	}

	for attempt, backoff := range sendRetryBackoff {
		if !isRetryableSendError(lastErr) {
			closeStream(current)
			return nil, fmt.Errorf("failed to send append request (%s): %w", grpcErrorDetail(lastErr), lastErr)
		}

		config.Debug(
			"[DEST] Worker %d: retryable send error, reopening stream for retry %d/%d after %s: %s",
			workerID,
			attempt+1,
			len(sendRetryBackoff),
			backoff,
			grpcErrorDetail(lastErr),
		)

		closeStream(current)
		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}

		var err error
		current, err = c.openStreamWithRetry(ctx, workerID)
		if err != nil {
			return nil, err
		}

		schemaBytes, schemaErr := serializeArrowSchema(recordSchema)
		if schemaErr != nil {
			closeStream(current)
			return nil, fmt.Errorf("failed to serialize arrow schema: %w", schemaErr)
		}
		req.Rows.(*storagepb.AppendRowsRequest_ArrowRows).ArrowRows.WriterSchema = &storagepb.ArrowSchema{
			SerializedSchema: schemaBytes,
		}

		lastErr = send(current)
		if lastErr == nil {
			return current, nil
		}
	}

	closeStream(current)
	return nil, fmt.Errorf("failed to send append request after retries (%s): %w", grpcErrorDetail(lastErr), lastErr)
}

func takeStreamRecvError(ss *streamState) error {
	if ss == nil {
		return nil
	}
	select {
	case err := <-ss.recvErr:
		return err
	default:
		return nil
	}
}

// appendArrowStreamWorker is a worker that handles a subset of records on its own stream
func (c *StorageWriteArrowClient) appendArrowStreamWorker(ctx context.Context, tablePath string, records <-chan arrow.RecordBatch, workerID int) error {
	needSchema := true
	var ss *streamState

	for record := range records {
		if err := takeStreamRecvError(ss); err != nil {
			closeStream(ss)
			if record != nil {
				record.Release()
			}
			return err
		}

		if ss == nil {
			var err error
			ss, err = c.openStreamWithRetry(ctx, workerID)
			if err != nil {
				return err
			}
		}

		if record == nil {
			continue
		}

		if record.NumRows() == 0 {
			record.Release()
			continue
		}

		var writerSchema *storagepb.ArrowSchema
		if needSchema {
			schemaBytes, err := serializeArrowSchema(record.Schema())
			if err != nil {
				record.Release()
				closeStream(ss)
				return fmt.Errorf("failed to serialize arrow schema: %w", err)
			}
			writerSchema = &storagepb.ArrowSchema{
				SerializedSchema: schemaBytes,
			}
			needSchema = false
		}

		data, serErr := serializeArrowRecordBatch(record)
		if serErr != nil {
			record.Release()
			closeStream(ss)
			return fmt.Errorf("failed to serialize record batch: %w", serErr)
		}

		if len(data) <= maxAppendRequestBytes {
			req := &storagepb.AppendRowsRequest{
				WriteStream: tablePath,
				Rows: &storagepb.AppendRowsRequest_ArrowRows{
					ArrowRows: &storagepb.AppendRowsRequest_ArrowData{
						WriterSchema: writerSchema,
						Rows: &storagepb.ArrowRecordBatch{
							SerializedRecordBatch: data,
						},
					},
				},
			}
			writerSchema = nil
			nextStream, err := c.sendRequestWithRetry(ctx, workerID, ss, req, record.Schema())
			if err != nil {
				record.Release()
				return err
			}
			ss = nextStream
			needSchema = false
		} else {
			subBatches := splitRecordBatch(record, maxAppendRequestBytes)
			for _, sub := range subBatches {
				req := &storagepb.AppendRowsRequest{
					WriteStream: tablePath,
					Rows: &storagepb.AppendRowsRequest_ArrowRows{
						ArrowRows: &storagepb.AppendRowsRequest_ArrowData{
							WriterSchema: writerSchema,
							Rows: &storagepb.ArrowRecordBatch{
								SerializedRecordBatch: sub.data,
							},
						},
					},
				}
				writerSchema = nil
				nextStream, err := c.sendRequestWithRetry(ctx, workerID, ss, req, record.Schema())
				if err != nil {
					sub.record.Release()
					record.Release()
					return err
				}
				ss = nextStream
				needSchema = false
				sub.record.Release()
			}
		}

		record.Release()

		select {
		case err := <-ss.recvErr:
			closeStream(ss)
			return err
		default:
		}
	}

	if ss != nil {
		if err := ss.stream.CloseSend(); err != nil && ctx.Err() == nil {
			return fmt.Errorf("failed to close send: %w", err)
		}

		<-ss.recvDone

		select {
		case err := <-ss.recvErr:
			return err
		default:
		}
	}

	return nil
}

func serializeArrowSchema(schema *arrow.Schema) ([]byte, error) {
	payload := ipc.GetSchemaPayload(schema, memory.NewGoAllocator())
	defer payload.Release()

	var buf bytes.Buffer
	if _, err := payload.WritePayload(&buf); err != nil {
		return nil, fmt.Errorf("failed to serialize arrow schema: %w", err)
	}
	return buf.Bytes(), nil
}

var serBufPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 2*1024*1024)) // 2MB initial
	},
}

func serializeArrowRecordBatch(record arrow.RecordBatch) ([]byte, error) {
	payload, err := ipc.GetRecordBatchPayload(record, ipc.WithAllocator(memory.NewGoAllocator()))
	if err != nil {
		return nil, fmt.Errorf("failed to get record batch payload: %w", err)
	}
	defer payload.Release()

	buf := serBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if _, err := payload.WritePayload(buf); err != nil {
		serBufPool.Put(buf)
		return nil, fmt.Errorf("failed to serialize arrow record batch: %w", err)
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	serBufPool.Put(buf)
	return result, nil
}

type splitBatch struct {
	record arrow.RecordBatch
	data   []byte // pre-serialized bytes, ready to send
}

// splitRecordBatch splits a record batch into sub-batches whose serialized size
// fits within maxBytes. Each returned splitBatch contains a NewSlice view (that
// the caller must Release) and its pre-serialized bytes so the caller can skip
// re-serialization. The original record is NOT released by this function.
func splitRecordBatch(record arrow.RecordBatch, maxBytes int) []splitBatch {
	numRows := record.NumRows()

	data, err := serializeArrowRecordBatch(record)
	if err != nil || len(data) <= maxBytes || numRows <= 1 {
		return []splitBatch{{record: record.NewSlice(0, numRows), data: data}}
	}

	mid := numRows / 2
	left := record.NewSlice(0, mid)
	right := record.NewSlice(mid, numRows)

	result := splitRecordBatch(left, maxBytes)
	left.Release()
	result = append(result, splitRecordBatch(right, maxBytes)...)
	right.Release()
	return result
}
