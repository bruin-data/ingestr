package uri

import "testing"

// url.Parse rejects scheme characters ingestr allows (the underscore in
// ps_mysql), so Parse extracts the scheme itself and must handle compound and
// underscore schemes alike.
func TestParseSchemeVariants(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantScheme string
		wantHost   string
		wantDB     string
		wantParams map[string]string
	}{
		{
			name:       "plain scheme",
			uri:        "mysql://user:pass@localhost:3306/testdb",
			wantScheme: "mysql",
			wantHost:   "localhost",
			wantDB:     "testdb",
		},
		{
			name:       "compound scheme",
			uri:        "mysql+pymysql://user:pass@localhost:3306/testdb",
			wantScheme: "mysql+pymysql",
			wantHost:   "localhost",
			wantDB:     "testdb",
		},
		{
			name:       "underscore scheme",
			uri:        "ps_mysql://user:pass@aws.connect.psdb.cloud:3306/mydb",
			wantScheme: "ps_mysql",
			wantHost:   "aws.connect.psdb.cloud",
			wantDB:     "mydb",
		},
		{
			name:       "underscore compound scheme with params",
			uri:        "ps_mysql+cdc://user:pass@aws.connect.psdb.cloud:3306/mydb?mode=batch",
			wantScheme: "ps_mysql+cdc",
			wantHost:   "aws.connect.psdb.cloud",
			wantDB:     "mydb",
			wantParams: map[string]string{"mode": "batch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse(tt.uri)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.uri, err)
			}
			if parsed.Scheme != tt.wantScheme {
				t.Errorf("Scheme: got %q want %q", parsed.Scheme, tt.wantScheme)
			}
			if parsed.Host != tt.wantHost {
				t.Errorf("Host: got %q want %q", parsed.Host, tt.wantHost)
			}
			if parsed.Database != tt.wantDB {
				t.Errorf("Database: got %q want %q", parsed.Database, tt.wantDB)
			}
			for k, v := range tt.wantParams {
				if parsed.Params[k] != v {
					t.Errorf("Params[%q]: got %q want %q", k, parsed.Params[k], v)
				}
			}
		})
	}
}
