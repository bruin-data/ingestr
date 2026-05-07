package asana

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantWorkspaceID string
		wantToken       string
		wantErr         bool
		errSubstr       string
	}{
		{
			name:            "valid uri",
			uri:             "asana://123456789?access_token=mytoken",
			wantWorkspaceID: "123456789",
			wantToken:       "mytoken",
		},
		{
			name:            "token with special characters",
			uri:             "asana://123456789?access_token=2/abc123:xyz456",
			wantWorkspaceID: "123456789",
			wantToken:       "2/abc123:xyz456",
		},
		{
			name:      "missing access_token",
			uri:       "asana://123456789",
			wantErr:   true,
			errSubstr: "access_token is required",
		},
		{
			name:      "empty access_token",
			uri:       "asana://123456789?access_token=",
			wantErr:   true,
			errSubstr: "access_token is required",
		},
		{
			name:      "missing workspace_id",
			uri:       "asana://?access_token=mytoken",
			wantErr:   true,
			errSubstr: "workspace_id is required",
		},
		{
			name:      "wrong scheme",
			uri:       "https://123456789?access_token=mytoken",
			wantErr:   true,
			errSubstr: "must start with asana://",
		},
		{
			name:      "empty uri",
			uri:       "",
			wantErr:   true,
			errSubstr: "must start with asana://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceID, token, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantWorkspaceID, workspaceID)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Tasks"))
	assert.False(t, isValidTable("WORKSPACES"))
}
