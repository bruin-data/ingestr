package kinesis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseKinesisURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(t *testing.T, c kinesisCredentials)
	}{
		{
			name: "valid with static credentials",
			uri:  "kinesis://?aws_access_key_id=AKID&aws_secret_access_key=SECRET&region_name=us-east-1",
			check: func(t *testing.T, c kinesisCredentials) {
				if c.AccessKeyID != "AKID" {
					t.Errorf("AccessKeyID = %q, want %q", c.AccessKeyID, "AKID")
				}
				if c.SecretAccessKey != "SECRET" {
					t.Errorf("SecretAccessKey = %q, want %q", c.SecretAccessKey, "SECRET")
				}
				if c.Region != "us-east-1" {
					t.Errorf("Region = %q, want %q", c.Region, "us-east-1")
				}
			},
		},
		{
			name: "host endpoint",
			uri:  "kinesis://172.17.0.1:4566?aws_access_key_id=AKID&aws_secret_access_key=SECRET&region_name=us-east-1",
			check: func(t *testing.T, c kinesisCredentials) {
				if c.EndpointURL != "http://172.17.0.1:4566" {
					t.Errorf("EndpointURL = %q, want %q", c.EndpointURL, "http://172.17.0.1:4566")
				}
				if c.AccessKeyID != "AKID" || c.SecretAccessKey != "SECRET" || c.Region != "us-east-1" {
					t.Errorf("unexpected credentials: %#v", c)
				}
			},
		},
		{
			name:    "wrong scheme",
			uri:     "kafka://?aws_access_key_id=AKID&aws_secret_access_key=SECRET&region_name=us-east-1",
			wantErr: true,
		},
		{
			name:    "missing access key",
			uri:     "kinesis://?aws_secret_access_key=SECRET&region_name=us-east-1",
			wantErr: true,
		},
		{
			name:    "missing secret key",
			uri:     "kinesis://?aws_access_key_id=AKID&region_name=us-east-1",
			wantErr: true,
		},
		{
			name:    "missing region",
			uri:     "kinesis://?aws_access_key_id=AKID&aws_secret_access_key=SECRET",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseKinesisURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, creds)
			}
		})
	}
}

func TestDigest128(t *testing.T) {
	result := digest128("shardId-000000000000" + "49590338271490256608559692538361571095921575989136588898")
	// base64 of 15 bytes = 20 chars
	if len(result) != 20 {
		t.Errorf("digest128 length = %d, want 20", len(result))
	}

	// Same input should produce same output
	result2 := digest128("shardId-000000000000" + "49590338271490256608559692538361571095921575989136588898")
	if result != result2 {
		t.Errorf("digest128 not deterministic: %q != %q", result, result2)
	}

	// Different input should produce different output
	result3 := digest128("shardId-000000000001" + "49590338271490256608559692538361571095921575989136588898")
	if result == result3 {
		t.Error("digest128 collision for different inputs")
	}
}

func TestBuildRecordItem_JSON(t *testing.T) {
	ts := time.Date(2023, 11, 14, 22, 13, 20, 123000000, time.UTC)
	record := types.Record{
		SequenceNumber:              aws.String("123456"),
		ApproximateArrivalTimestamp: &ts,
		Data:                        []byte(`{"user_id": 42, "event": "login"}`),
		PartitionKey:                aws.String("pk-1"),
	}

	item := buildRecordItem(record, "shardId-000", "my-stream")

	if item["kinesis_msg_id"] == nil {
		t.Fatal("kinesis_msg_id is nil")
	}
	msgID, ok := item["kinesis_msg_id"].(string)
	if !ok || len(msgID) != 20 {
		t.Errorf("kinesis_msg_id length = %d, want 20", len(msgID))
	}
	if item["kinesis"] == nil {
		t.Fatal("kinesis metadata is nil")
	}

	meta, ok := item["kinesis"].(map[string]interface{})
	if !ok {
		t.Fatal("kinesis metadata is not a map")
	}
	if meta["shard_id"] != "shardId-000" {
		t.Errorf("shard_id = %v, want shardId-000", meta["shard_id"])
	}
	if meta["stream_name"] != "my-stream" {
		t.Errorf("stream_name = %v, want my-stream", meta["stream_name"])
	}
	if meta["partition"] != "pk-1" {
		t.Errorf("partition = %v, want pk-1", meta["partition"])
	}
	// ts should be an ISO timestamp string
	tsStr, ok := meta["ts"].(string)
	if !ok {
		t.Fatalf("ts type = %T, want string", meta["ts"])
	}
	if tsStr != "2023-11-14T22:13:20.123000+00:00" {
		t.Errorf("ts = %q, want %q", tsStr, "2023-11-14T22:13:20.123000+00:00")
	}

	// JSON fields should be spread into top level
	if item["event"] != "login" {
		t.Errorf("event = %v, want login", item["event"])
	}
	// user_id should be a json.Number (UseNumber)
	if _, ok := item["user_id"].(json.Number); !ok {
		t.Errorf("user_id type = %T, want json.Number", item["user_id"])
	}
}

func TestBuildRecordItem_NonJSON(t *testing.T) {
	ts := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)
	record := types.Record{
		SequenceNumber:              aws.String("123456"),
		ApproximateArrivalTimestamp: &ts,
		Data:                        []byte(`not json data`),
		PartitionKey:                aws.String("pk-1"),
	}

	item := buildRecordItem(record, "shardId-000", "my-stream")

	if item["data"] != "not json data" {
		t.Errorf("data = %v, want 'not json data'", item["data"])
	}
}

func TestReadShardsConcurrentlyWithStreamingStartsAllShards(t *testing.T) {
	src := NewKinesisSource()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan string, 2)
	done := make(chan error, 1)
	read := func(ctx context.Context, shardID string) error {
		started <- shardID
		<-ctx.Done()
		return ctx.Err()
	}

	go func() {
		done <- src.readShardsConcurrentlyWith(ctx, []string{"child-a", "child-b"}, source.ReadOptions{Streaming: true}, read)
	}()

	got := make(map[string]bool, 2)
	deadline := time.After(time.Second)
	for len(got) < 2 {
		select {
		case shardID := <-started:
			got[shardID] = true
		case <-deadline:
			t.Fatalf("started shards = %v, want both child shards", got)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("readShardsConcurrentlyWith error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readShardsConcurrentlyWith did not stop after cancellation")
	}
}

func TestReadShardsConcurrentlyReturnsShardError(t *testing.T) {
	src := NewKinesisSource()
	shardErr := errors.New("boom")

	err := src.readShardsConcurrentlyWith(
		context.Background(),
		[]string{"shard-a"},
		source.ReadOptions{Streaming: true},
		func(context.Context, string) error {
			return shardErr
		},
	)
	if err == nil {
		t.Fatal("expected shard error")
	}
	if !errors.Is(err, shardErr) {
		t.Fatalf("error = %v, want wrapped shard error", err)
	}
	if strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, should not be replaced with context cancellation", err)
	}
}

func TestClaimStreamShardsSkipsDuplicates(t *testing.T) {
	src := NewKinesisSource()

	first := src.claimStreamShards([]string{"parent-a", "parent-b", "parent-a"})
	if len(first) != 2 || first[0] != "parent-a" || first[1] != "parent-b" {
		t.Fatalf("first claim = %v, want [parent-a parent-b]", first)
	}

	second := src.claimStreamShards([]string{"parent-b", "child-c"})
	if len(second) != 1 || second[0] != "child-c" {
		t.Fatalf("second claim = %v, want [child-c]", second)
	}
}
