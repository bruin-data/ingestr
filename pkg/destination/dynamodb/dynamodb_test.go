package dynamodb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

type dynamoHTTPClient func(*http.Request) (*http.Response, error)

func (f dynamoHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise SDK serialization and error decoding without AWS credentials or network access.
func testDestination(t *testing.T, handle func(string, map[string]any) (int, any)) *DynamoDBDestination {
	t.Helper()
	client := dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: aws.AnonymousCredentials{},
		HTTPClient: dynamoHTTPClient(func(r *http.Request) (*http.Response, error) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			status, response := handle(strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "DynamoDB_20120810."), body)
			data, err := json.Marshal(response)
			require.NoError(t, err)
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/x-amz-json-1.0"}},
				Body:       io.NopCloser(strings.NewReader(string(data))),
			}, nil
		}),
	}, func(o *dynamodb.Options) { o.RetryMaxAttempts = 1 })
	return &DynamoDBDestination{client: client}
}

func TestBatchWriteRetriesOnlyUnprocessedItems(t *testing.T) {
	items := []types.WriteRequest{
		{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "a"}}}},
		{DeleteRequest: &types.DeleteRequest{Key: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "b"}}}},
		{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "c"}}}},
	}
	a := map[string]any{"PutRequest": map[string]any{"Item": map[string]any{"id": map[string]any{"S": "a"}}}}
	b := map[string]any{"DeleteRequest": map[string]any{"Key": map[string]any{"id": map[string]any{"S": "b"}}}}
	c := map[string]any{"PutRequest": map[string]any{"Item": map[string]any{"id": map[string]any{"S": "c"}}}}
	wantRequests := [][]any{{a, b, c}, {c, b}, {b}}
	calls := 0
	d := testDestination(t, func(op string, body map[string]any) (int, any) {
		require.Equal(t, "BatchWriteItem", op)
		require.Less(t, calls, len(wantRequests))
		require.Equal(t, map[string]any{"target": wantRequests[calls]}, body["RequestItems"])
		calls++
		if calls == len(wantRequests) {
			return http.StatusOK, map[string]any{}
		}
		return http.StatusOK, map[string]any{"UnprocessedItems": map[string]any{"target": wantRequests[calls]}}
	})
	written, err := d.batchWrite(context.Background(), "target", items)
	require.NoError(t, err)
	require.EqualValues(t, 3, written)
	require.Equal(t, 3, calls)
	written, err = d.batchWrite(context.Background(), "target", nil)
	require.NoError(t, err)
	require.Zero(t, written)
	require.Equal(t, 3, calls)
}

func TestBatchWriteFailures(t *testing.T) {
	for _, mode := range []string{"exhausted", "cancelled", "API error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			d := testDestination(t, func(op string, body map[string]any) (int, any) {
				require.Equal(t, "BatchWriteItem", op)
				calls++
				if mode == "API error" {
					return http.StatusBadRequest, map[string]any{"__type": "ValidationException", "message": "invalid item"}
				}
				if mode == "cancelled" {
					cancel()
				}
				return http.StatusOK, map[string]any{"UnprocessedItems": body["RequestItems"]}
			})
			written, err := d.batchWrite(ctx, "target", []types.WriteRequest{
				{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "a"}}}},
			})
			require.Zero(t, written)
			switch mode {
			case "exhausted":
				require.EqualError(t, err, "failed to write all items after retries, 1 unprocessed")
				require.Equal(t, 5, calls)
			case "cancelled":
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, calls)
			case "API error":
				var validation smithy.APIError
				require.ErrorAs(t, err, &validation)
				require.Equal(t, "ValidationException", validation.ErrorCode())
				require.Equal(t, 1, calls)
			}
		})
	}
}

func cdcItem(id, lsn, value string, deleted bool) map[string]any {
	item := map[string]any{
		"id":                               map[string]any{"S": id},
		destination.CDCLSNColumn:           map[string]any{"S": lsn},
		destination.CDCDeletedColumn:       map[string]any{"BOOL": deleted},
		destination.CDCSyncedAtColumn:      map[string]any{"S": "synced-" + lsn},
		destination.CDCUnchangedColsColumn: map[string]any{"S": "[]"},
	}
	if value != "" {
		item["value"] = map[string]any{"S": value}
	}
	return item
}

func TestMergeCDC(t *testing.T) {
	// Reverse the entire scan, including page order, to catch last-seen-wins
	// implementations and losing delete tie-breaks in either direction.
	for _, reverse := range []bool{false, true} {
		name := "forward"
		if reverse {
			name = "reverse"
		}
		t.Run(name, func(t *testing.T) {
			rows := []map[string]any{
				cdcItem("updated", "0002", "new", false),
				cdcItem("deleted", "0003", "", true),
				cdcItem("resurrected", "0002", "", true),
				cdcItem("tied", "0004", "tie-data", false),
				cdcItem("delete-only", "0008", "", true),
				cdcItem("unknown", "0009", "", true),
				cdcItem("updated", "0001", "old", false),
				cdcItem("deleted", "0002", "last-active", false),
				cdcItem("deleted", "0001", "old", false),
				cdcItem("resurrected", "0003", "reborn", false),
				cdcItem("tied", "0004", "", true),
				cdcItem("delete-only", "0007", "", true),
			}
			if reverse {
				for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
					rows[i], rows[j] = rows[j], rows[i]
				}
			}
			scans, writes := 0, 0
			puts := map[string]any{}
			updates := map[string]any{}
			cursor := map[string]any{"id": map[string]any{"S": "cursor"}}
			d := testDestination(t, func(op string, body map[string]any) (int, any) {
				switch op {
				case "Scan":
					require.Equal(t, "staging", body["TableName"])
					scans++
					if scans == 1 {
						require.NotContains(t, body, "ExclusiveStartKey")
						return http.StatusOK, map[string]any{"Items": rows[:6], "LastEvaluatedKey": cursor}
					}
					require.Equal(t, 2, scans)
					require.Equal(t, cursor, body["ExclusiveStartKey"])
					return http.StatusOK, map[string]any{"Items": rows[6:]}
				case "BatchWriteItem":
					writes++
					requests := body["RequestItems"].(map[string]any)
					require.Len(t, requests, 1)
					for _, request := range requests["target"].([]any) {
						item := request.(map[string]any)["PutRequest"].(map[string]any)["Item"].(map[string]any)
						id := item["id"].(map[string]any)["S"].(string)
						require.NotContains(t, puts, id)
						puts[id] = item
					}
					return http.StatusOK, map[string]any{}
				case "UpdateItem":
					require.Equal(t, "target", body["TableName"])
					key := body["Key"].(map[string]any)
					require.Len(t, key, 1)
					id := key["id"].(map[string]any)["S"].(string)
					require.NotContains(t, updates, id)
					updates[id] = body["ExpressionAttributeValues"]
					require.Equal(t, "attribute_exists(#pk)", body["ConditionExpression"])
					require.Equal(t, "SET #del = :del, #lsn = :lsn, #syn = :syn", body["UpdateExpression"])
					require.Equal(t, map[string]any{"#pk": "id", "#del": "_cdc_deleted", "#lsn": "_cdc_lsn", "#syn": "_cdc_synced_at"}, body["ExpressionAttributeNames"])
					if id == "unknown" {
						return http.StatusBadRequest, map[string]any{"__type": "ConditionalCheckFailedException", "message": "item does not exist"}
					}
					return http.StatusOK, map[string]any{}
				default:
					t.Fatalf("unexpected operation %s", op)
					return 0, nil
				}
			})
			err := d.MergeTable(context.Background(), destination.MergeOptions{
				TargetTable: "target", StagingTable: "staging", PrimaryKeys: []string{"id"},
				Columns: []string{"id", "value", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"},
			})
			require.NoError(t, err)
			require.Equal(t, 2, scans)
			require.Equal(t, 1, writes)
			require.Len(t, puts, 4)
			for _, want := range []map[string]any{
				cdcItem("updated", "0002", "new", false),
				cdcItem("deleted", "0003", "last-active", true),
				cdcItem("resurrected", "0003", "reborn", false),
				cdcItem("tied", "0004", "tie-data", true),
			} {
				delete(want, destination.CDCUnchangedColsColumn)
				id := want["id"].(map[string]any)["S"].(string)
				require.Equal(t, want, puts[id])
			}
			require.Equal(t, map[string]any{
				"delete-only": map[string]any{":del": map[string]any{"BOOL": true}, ":lsn": map[string]any{"S": "0008"}, ":syn": map[string]any{"S": "synced-0008"}},
				"unknown":     map[string]any{":del": map[string]any{"BOOL": true}, ":lsn": map[string]any{"S": "0009"}, ":syn": map[string]any{"S": "synced-0009"}},
			}, updates)
		})
	}
}
