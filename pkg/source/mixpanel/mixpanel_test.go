package mixpanel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      mixpanelCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid URI with username/password",
			uri:  "mixpanel://?username=sa_user&password=sa_secret&project_id=12345&server=us",
			want: mixpanelCredentials{username: "sa_user", password: "sa_secret", projectID: "12345", server: "us"},
		},
		{
			name: "valid URI with api_secret",
			uri:  "mixpanel://?api_secret=my_secret&project_id=12345&server=eu",
			want: mixpanelCredentials{username: "my_secret", password: "", projectID: "12345", server: "eu"},
		},
		{
			name: "default server is eu",
			uri:  "mixpanel://?username=u&password=p&project_id=99",
			want: mixpanelCredentials{username: "u", password: "p", projectID: "99", server: "eu"},
		},
		{
			name: "server in",
			uri:  "mixpanel://?username=u&password=p&project_id=99&server=in",
			want: mixpanelCredentials{username: "u", password: "p", projectID: "99", server: "in"},
		},
		{
			name: "username takes precedence over api_secret",
			uri:  "mixpanel://?username=u&password=p&api_secret=secret&project_id=99",
			want: mixpanelCredentials{username: "u", password: "p", projectID: "99", server: "eu"},
		},
		{
			name:      "missing credentials",
			uri:       "mixpanel://?project_id=12345",
			wantErr:   true,
			errSubstr: "either username/password or api_secret is required",
		},
		{
			name:      "missing project_id with service account",
			uri:       "mixpanel://?username=u&password=p",
			wantErr:   true,
			errSubstr: "project_id is required",
		},
		{
			name: "api_secret without project_id is valid",
			uri:  "mixpanel://?api_secret=secret123",
			want: mixpanelCredentials{username: "secret123", password: "", projectID: "", server: "eu"},
		},
		{
			name:      "invalid server",
			uri:       "mixpanel://?username=u&password=p&project_id=99&server=jp",
			wantErr:   true,
			errSubstr: "invalid server",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?username=u&password=p&project_id=99",
			wantErr:   true,
			errSubstr: "must start with mixpanel://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.username, got.username)
			assert.Equal(t, tt.want.password, got.password)
			assert.Equal(t, tt.want.projectID, got.projectID)
			assert.Equal(t, tt.want.server, got.server)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Events"))
	assert.False(t, isValidTable("PROFILES"))
}

func TestExportBaseURL(t *testing.T) {
	assert.Equal(t, "https://data.mixpanel.com", exportBaseURL("us"))
	assert.Equal(t, "https://data-eu.mixpanel.com", exportBaseURL("eu"))
	assert.Equal(t, "https://data-in.mixpanel.com", exportBaseURL("in"))
}

func TestEngageBaseURL(t *testing.T) {
	assert.Equal(t, "https://mixpanel.com", engageBaseURL("us"))
	assert.Equal(t, "https://eu.mixpanel.com", engageBaseURL("eu"))
	assert.Equal(t, "https://in.mixpanel.com", engageBaseURL("in"))
}

func TestFlattenProperties(t *testing.T) {
	t.Run("flattens and strips $ prefix", func(t *testing.T) {
		event := map[string]interface{}{
			"event": "signup",
			"properties": map[string]interface{}{
				"$distinct_id": "user_001",
				"$insert_id":   "ins_001",
				"time":         json.Number("1711036800"),
				"plan":         "free",
			},
		}
		NewMixpanelSource().flattenProperties(event)

		assert.Equal(t, "signup", event["event"])
		assert.Equal(t, "user_001", event["distinct_id"])
		assert.Equal(t, "ins_001", event["insert_id"])
		assert.Equal(t, json.Number("1711036800"), event["time"])
		assert.Equal(t, "free", event["plan"])
		assert.Nil(t, event["properties"])
	})

	t.Run("no properties key", func(t *testing.T) {
		event := map[string]interface{}{"event": "test"}
		NewMixpanelSource().flattenProperties(event)
		assert.Equal(t, "test", event["event"])
	})
}

func TestFlattenProfileProperties(t *testing.T) {
	t.Run("flattens profile and strips $ prefix", func(t *testing.T) {
		profile := map[string]interface{}{
			"$distinct_id": "user_001",
			"$properties": map[string]interface{}{
				"$first_name": "Alice",
				"$last_name":  "Smith",
				"$email":      "alice@example.com",
				"$last_seen":  "2026-03-24T04:42:54",
				"plan":        "free",
			},
		}
		flattenProfileProperties(profile)

		assert.Equal(t, "user_001", profile["distinct_id"])
		assert.Equal(t, "Alice", profile["first_name"])
		assert.Equal(t, "Smith", profile["last_name"])
		assert.Equal(t, "alice@example.com", profile["email"])
		assert.Equal(t, "2026-03-24T04:42:54Z", profile["last_seen"])
		assert.Equal(t, "free", profile["plan"])
		assert.Nil(t, profile["$distinct_id"])
		assert.Nil(t, profile["$properties"])
	})

	t.Run("handles RFC3339 last_seen", func(t *testing.T) {
		profile := map[string]interface{}{
			"$distinct_id": "u1",
			"$properties": map[string]interface{}{
				"$last_seen": "2026-03-24T04:42:54+00:00",
			},
		}
		flattenProfileProperties(profile)
		assert.Equal(t, "2026-03-24T04:42:54Z", profile["last_seen"])
	})
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		data := []byte(`{"id": 2033513821949367296, "name": "test"}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		id, ok := result["id"].(json.Number)
		require.True(t, ok, "id should be json.Number, got %T", result["id"])
		assert.Equal(t, "2033513821949367296", id.String())

		i, err := id.Int64()
		require.NoError(t, err)
		assert.Equal(t, int64(2033513821949367296), i)
	})

	t.Run("preserves floats", func(t *testing.T) {
		data := []byte(`{"score": 3.14}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		score, ok := result["score"].(json.Number)
		require.True(t, ok)
		f, err := score.Float64()
		require.NoError(t, err)
		assert.InDelta(t, 3.14, f, 0.001)
	})

	t.Run("handles arrays", func(t *testing.T) {
		data := []byte(`[{"id": 1}, {"id": 2}]`)
		var result []map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.Error(t, err)
	})
}
