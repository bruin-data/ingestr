//go:build integration

package slack_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestSlackPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	token := os.Getenv("SLACK_API_KEY")
	if token == "" {
		t.Skip("Set SLACK_API_KEY to run Slack integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("slack://?api_key=%s", token)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("slack_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:    "channels",
			DestTable:      "main.slack_channels",
			KeyColumn:      "id",
			ExcludeColumns: []string{"updated"},
			ExpectedSchema: []schema.Column{
				{Name: "context_team_id", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "creator", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_archived", DataType: schema.TypeBoolean},
				{Name: "is_channel", DataType: schema.TypeBoolean},
				{Name: "is_ext_shared", DataType: schema.TypeBoolean},
				{Name: "is_general", DataType: schema.TypeBoolean},
				{Name: "is_group", DataType: schema.TypeBoolean},
				{Name: "is_im", DataType: schema.TypeBoolean},
				{Name: "is_member", DataType: schema.TypeBoolean},
				{Name: "is_mpim", DataType: schema.TypeBoolean},
				{Name: "is_org_shared", DataType: schema.TypeBoolean},
				{Name: "is_pending_ext_shared", DataType: schema.TypeBoolean},
				{Name: "is_private", DataType: schema.TypeBoolean},
				{Name: "is_shared", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "name_normalized", DataType: schema.TypeString},
				{Name: "num_members", DataType: schema.TypeInt64},
				{Name: "parent_conversation", DataType: schema.TypeString},
				{Name: "pending_connected_team_ids", DataType: schema.TypeJSON},
				{Name: "pending_shared", DataType: schema.TypeJSON},
				{Name: "previous_names", DataType: schema.TypeJSON},
				{Name: "properties", DataType: schema.TypeJSON},
				{Name: "purpose", DataType: schema.TypeJSON},
				{Name: "shared_team_ids", DataType: schema.TypeJSON},
				{Name: "topic", DataType: schema.TypeJSON},
				{Name: "unlinked", DataType: schema.TypeInt64},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "C0AFEEM34JK",
					Fields: map[string]any{
						"context_team_id":       "T0AFPGBGLKD",
						"created":               time.Date(2026, 2, 19, 7, 56, 3, 0, time.UTC),
						"creator":               "U0AG8RDNDA5",
						"is_archived":           false,
						"is_channel":            true,
						"is_ext_shared":         false,
						"is_general":            true,
						"is_group":              false,
						"is_im":                 false,
						"is_member":             true,
						"is_mpim":               false,
						"is_org_shared":         false,
						"is_pending_ext_shared": false,
						"is_private":            false,
						"is_shared":             false,
						"name":                  "all-getbruin",
						"name_normalized":       "all-getbruin",
						"num_members":           float64(2),
						"unlinked":              float64(0),
						"updated":               time.Date(2026, 2, 19, 7, 56, 19, 0, time.UTC),
					},
				},
				{
					ID: "C0AFSFBHVS9",
					Fields: map[string]any{
						"context_team_id":       "T0AFPGBGLKD",
						"created":               time.Date(2026, 2, 19, 7, 56, 3, 0, time.UTC),
						"creator":               "U0AG8RDNDA5",
						"is_archived":           false,
						"is_channel":            true,
						"is_ext_shared":         false,
						"is_general":            false,
						"is_group":              false,
						"is_im":                 false,
						"is_member":             false,
						"is_mpim":               false,
						"is_org_shared":         false,
						"is_pending_ext_shared": false,
						"is_private":            false,
						"is_shared":             false,
						"name":                  "social",
						"name_normalized":       "social",
						"num_members":           float64(1),
						"unlinked":              float64(0),
						"updated":               time.Date(2026, 2, 19, 7, 56, 3, 0, time.UTC),
					},
				},
				{
					ID: "C0AFTS51QDU",
					Fields: map[string]any{
						"context_team_id":       "T0AFPGBGLKD",
						"created":               time.Date(2026, 2, 19, 7, 56, 34, 0, time.UTC),
						"creator":               "U0AG8RDNDA5",
						"is_archived":           false,
						"is_channel":            true,
						"is_ext_shared":         false,
						"is_general":            false,
						"is_group":              false,
						"is_im":                 false,
						"is_member":             true,
						"is_mpim":               false,
						"is_org_shared":         false,
						"is_pending_ext_shared": false,
						"is_private":            false,
						"is_shared":             false,
						"name":                  "new-channel",
						"name_normalized":       "new-channel",
						"num_members":           float64(2),
						"unlinked":              float64(0),
						"updated":               time.Date(2026, 2, 19, 7, 56, 34, 0, time.UTC),
					},
				},
			},
		},
		{
			SourceTable:    "users",
			DestTable:      "main.slack_users",
			KeyColumn:      "id",
			ExcludeColumns: []string{"updated"},
			ExpectedSchema: []schema.Column{
				{Name: "color", DataType: schema.TypeString},
				{Name: "deleted", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_admin", DataType: schema.TypeBoolean},
				{Name: "is_app_user", DataType: schema.TypeBoolean},
				{Name: "is_bot", DataType: schema.TypeBoolean},
				{Name: "is_email_confirmed", DataType: schema.TypeBoolean},
				{Name: "is_owner", DataType: schema.TypeBoolean},
				{Name: "is_primary_owner", DataType: schema.TypeBoolean},
				{Name: "is_restricted", DataType: schema.TypeBoolean},
				{Name: "is_ultra_restricted", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "profile", DataType: schema.TypeJSON},
				{Name: "real_name", DataType: schema.TypeString},
				{Name: "team_id", DataType: schema.TypeString},
				{Name: "tz", DataType: schema.TypeString},
				{Name: "tz_label", DataType: schema.TypeString},
				{Name: "tz_offset", DataType: schema.TypeInt64},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
				{Name: "who_can_share_contact_card", DataType: schema.TypeString},
				{Name: "locale", DataType: schema.TypeString},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "USLACKBOT",
					Fields: map[string]any{
						"color":                      "757575",
						"deleted":                    false,
						"is_admin":                   false,
						"is_app_user":                false,
						"is_bot":                     false,
						"is_email_confirmed":         false,
						"is_owner":                   false,
						"is_primary_owner":           false,
						"is_restricted":              false,
						"is_ultra_restricted":        false,
						"name":                       "slackbot",
						"real_name":                  "Slackbot",
						"team_id":                    "T0AFPGBGLKD",
						"tz":                         "America/Los_Angeles",
						"tz_label":                   "Pacific Standard Time",
						"tz_offset":                  float64(-28800),
						"updated":                    time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
						"who_can_share_contact_card": "EVERYONE",
					},
				},
				{
					ID: "U0AFTTLAKUN",
					Fields: map[string]any{
						"color":                      "9e3997",
						"deleted":                    false,
						"is_admin":                   false,
						"is_app_user":                false,
						"is_bot":                     true,
						"is_email_confirmed":         false,
						"is_owner":                   false,
						"is_primary_owner":           false,
						"is_restricted":              false,
						"is_ultra_restricted":        false,
						"name":                       "gong_test",
						"real_name":                  "Gong Test",
						"team_id":                    "T0AFPGBGLKD",
						"tz":                         "America/Los_Angeles",
						"tz_label":                   "Pacific Standard Time",
						"tz_offset":                  float64(-28800),
						"updated":                    time.Date(2026, 2, 19, 8, 9, 45, 0, time.UTC),
						"who_can_share_contact_card": "EVERYONE",
						"locale":                     "en-US",
					},
				},
				{
					ID: "U0AG8RDNDA5",
					Fields: map[string]any{
						"color":                      "84b22f",
						"deleted":                    false,
						"is_admin":                   true,
						"is_app_user":                false,
						"is_bot":                     false,
						"is_email_confirmed":         true,
						"is_owner":                   true,
						"is_primary_owner":           true,
						"is_restricted":              false,
						"is_ultra_restricted":        false,
						"name":                       "vendor_accounts",
						"real_name":                  "vendor_accounts",
						"team_id":                    "T0AFPGBGLKD",
						"tz":                         "Asia/Istanbul",
						"tz_label":                   "Turkey Time",
						"tz_offset":                  float64(10800),
						"updated":                    time.Date(2026, 2, 19, 7, 56, 22, 0, time.UTC),
						"who_can_share_contact_card": "EVERYONE",
						"locale":                     "en-US",
					},
				},
			},
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}

func TestSlackMessagesPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	token := os.Getenv("SLACK_API_KEY")
	if token == "" {
		t.Skip("Set SLACK_API_KEY to run Slack integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("slack://?api_key=%s", token)
	channelID := "new-channel"

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("slack_messages_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	exp := testutil.TableExpectation{
		SourceTable:         fmt.Sprintf("messages:%s", channelID),
		DestTable:           "main.slack_messages",
		KeyColumn:           "ts",
		MinExpectedRowCount: 1,
		ExcludeColumns:      []string{"subtype"},
		ExpectedSchema: []schema.Column{
			{Name: "app_id", DataType: schema.TypeString},
			{Name: "blocks", DataType: schema.TypeJSON},
			{Name: "bot_id", DataType: schema.TypeString},
			{Name: "bot_profile", DataType: schema.TypeJSON},
			{Name: "channel", DataType: schema.TypeString},
			{Name: "subtype", DataType: schema.TypeString},
			{Name: "team", DataType: schema.TypeString},
			{Name: "text", DataType: schema.TypeString},
			{Name: "ts", DataType: schema.TypeTimestampTZ},
			{Name: "type", DataType: schema.TypeString},
			{Name: "user", DataType: schema.TypeString},
		},
	}

	ts1, chID := postSlackMessage(t, token, channelID, "gong test message 1")
	t.Cleanup(func() { deleteSlackMessage(t, token, chID, ts1) })
	testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
	testutil.Check(t, destURI, exp)
}

func postSlackMessage(t *testing.T, token, channel, text string) (ts, resolvedChannel string) {
	t.Helper()

	form := url.Values{}
	form.Set("channel", channel)
	form.Set("text", text)

	req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", nil)
	require.NoError(t, err)
	req.URL.RawQuery = form.Encode()
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		TS      string `json:"ts"`
		Channel string `json:"channel"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.True(t, result.OK, "chat.postMessage failed: %s", result.Error)

	return result.TS, result.Channel
}

func deleteSlackMessage(t *testing.T, token, channel, ts string) {
	t.Helper()

	form := url.Values{}
	form.Set("channel", channel)
	form.Set("ts", ts)

	req, err := http.NewRequest("POST", "https://slack.com/api/chat.delete", nil)
	if err != nil {
		t.Logf("failed to create delete request: %v", err)
		return
	}
	req.URL.RawQuery = form.Encode()
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("failed to delete message ts=%s: %v", ts, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
}
