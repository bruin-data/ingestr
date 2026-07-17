//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	natssource "github.com/bruin-data/ingestr/pkg/source/nats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNATS_ToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsURI := startNATSContainer(t, ctx)
	nc, js := newNATSClient(t, natsURI)
	defer nc.Close()

	stream := fmt.Sprintf("SQLITE_%d", time.Now().UnixNano())
	subject := fmt.Sprintf("events.sqlite.%d", time.Now().UnixNano())
	createNATSStream(t, js, stream, subject)
	publishNATSMessages(t, js, subject, 1, 25)

	tmpFile, err := os.CreateTemp("", "nats_test_*.db")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

	cfg := &config.IngestConfig{
		SourceURI: fmt.Sprintf(
			"%s?subject=%s&batch_size=10&batch_timeout=0.2",
			natsURI,
			url.QueryEscape(subject),
		),
		SourceTable:         stream,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "messages",
		IncrementalStrategy: config.StrategyReplace,
		Progress:            config.ProgressLog,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count, distinctIDs, metadataRows int
	var minSeq, maxSeq int
	require.NoError(t, db.QueryRow(`
		SELECT
			COUNT(*),
			COUNT(DISTINCT nats_msg_id),
			MIN(CAST(json_extract(data, '$.seq') AS INTEGER)),
			MAX(CAST(json_extract(data, '$.seq') AS INTEGER)),
			COUNT(CASE
				WHEN json_extract(nats, '$.stream') = ?
				 AND json_extract(nats, '$.subject') = ? THEN 1
			END)
		FROM messages`, stream, subject).Scan(&count, &distinctIDs, &minSeq, &maxSeq, &metadataRows))

	assert.Equal(t, 25, count)
	assert.Equal(t, count, distinctIDs)
	assert.Equal(t, 1, minSeq)
	assert.Equal(t, 25, maxSeq)
	assert.Equal(t, count, metadataRows)
}

func TestNATS_CancellationInterruptsFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsURI := startNATSContainer(t, ctx)
	nc, js := newNATSClient(t, natsURI)
	defer nc.Close()

	stream := fmt.Sprintf("CANCEL_%d", time.Now().UnixNano())
	subject := fmt.Sprintf("events.cancel.%d", time.Now().UnixNano())
	createNATSStream(t, js, stream, subject)

	src := natssource.NewNATSSource()
	require.NoError(t, src.Connect(ctx, fmt.Sprintf("%s?subject=%s&batch_timeout=30", natsURI, url.QueryEscape(subject))))
	defer func() { _ = src.Close(ctx) }()

	table, err := src.GetTable(ctx, source.TableRequest{Name: stream, Streaming: true})
	require.NoError(t, err)
	readCtx, cancel := context.WithCancel(ctx)
	records, err := table.Read(readCtx, source.ReadOptions{Streaming: true})
	require.NoError(t, err)

	started := time.Now()
	cancel()
	select {
	case _, ok := <-records:
		assert.False(t, ok, "results channel should close without an error result")
		assert.Less(t, time.Since(started), 2*time.Second)
	case <-time.After(2 * time.Second):
		t.Fatal("NATS fetch did not stop promptly after context cancellation")
	}
}
