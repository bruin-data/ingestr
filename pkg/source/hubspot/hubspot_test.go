package hubspot

import (
	"reflect"
	"testing"
)

func TestParseHistoryTableName(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantBase  string
		wantProps []string
	}{
		{
			name:      "non-history unchanged",
			input:     "contacts",
			wantBase:  "contacts",
			wantProps: nil,
		},
		{
			name:      "non-history custom unchanged",
			input:     "custom:myObj:assoc1,assoc2",
			wantBase:  "custom:myObj:assoc1,assoc2",
			wantProps: nil,
		},
		{
			name:      "builtin history no suffix",
			input:     "property_history:contacts",
			wantBase:  "property_history:contacts",
			wantProps: nil,
		},
		{
			name:      "builtin history single prop",
			input:     "property_history:contacts:email",
			wantBase:  "property_history:contacts",
			wantProps: []string{"email"},
		},
		{
			name:      "builtin history multiple props",
			input:     "property_history:contacts:email,firstname,lastname",
			wantBase:  "property_history:contacts",
			wantProps: []string{"email", "firstname", "lastname"},
		},
		{
			name:      "builtin history trailing comma",
			input:     "property_history:contacts:email,firstname,",
			wantBase:  "property_history:contacts",
			wantProps: []string{"email", "firstname"},
		},
		{
			name:      "builtin history whitespace",
			input:     "property_history:contacts: email , firstname ",
			wantBase:  "property_history:contacts",
			wantProps: []string{"email", "firstname"},
		},
		{
			name:      "builtin history empty suffix",
			input:     "property_history:contacts:",
			wantBase:  "property_history:contacts",
			wantProps: nil,
		},
		{
			name:      "custom history no suffix",
			input:     "property_history:custom:myObj",
			wantBase:  "property_history:custom:myObj",
			wantProps: nil,
		},
		{
			name:      "custom history with props",
			input:     "property_history:custom:myObj:p1,p2",
			wantBase:  "property_history:custom:myObj",
			wantProps: []string{"p1", "p2"},
		},
		{
			name:      "custom history only commas",
			input:     "property_history:custom:myObj:,,,",
			wantBase:  "property_history:custom:myObj",
			wantProps: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBase, gotProps := parseHistoryTableName(tc.input)
			if gotBase != tc.wantBase {
				t.Errorf("base: got %q, want %q", gotBase, tc.wantBase)
			}
			if !reflect.DeepEqual(gotProps, tc.wantProps) {
				t.Errorf("props: got %#v, want %#v", gotProps, tc.wantProps)
			}
		})
	}
}

func TestParseHubspotURI(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "api_key",
			input: "hubspot://?api_key=pat_test_12345",
			want:  "pat_test_12345",
		},
		{
			name:  "service_key",
			input: "hubspot://?service_key=sk_test_67890",
			want:  "sk_test_67890",
		},
		{
			name:  "both equal",
			input: "hubspot://?api_key=tok&service_key=tok",
			want:  "tok",
		},
		{
			name:    "both differ",
			input:   "hubspot://?api_key=a&service_key=b",
			wantErr: true,
		},
		{
			name:    "missing credential",
			input:   "hubspot://?",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "hubspot://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			input:   "postgres://?api_key=x",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHubspotURI(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseTableAssocOverride(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantBase     string
		wantOverride []string
		wantOK       bool
	}{
		{
			name:         "no colon",
			input:        "contacts",
			wantBase:     "contacts",
			wantOverride: nil,
			wantOK:       false,
		},
		{
			name:         "single override",
			input:        "contacts:companies",
			wantBase:     "contacts",
			wantOverride: []string{"companies"},
			wantOK:       true,
		},
		{
			name:         "multiple overrides",
			input:        "contacts:companies,deals,tickets",
			wantBase:     "contacts",
			wantOverride: []string{"companies", "deals", "tickets"},
			wantOK:       true,
		},
		{
			name:         "empty override means no associations",
			input:        "contacts:",
			wantBase:     "contacts",
			wantOverride: []string{},
			wantOK:       true,
		},
		{
			name:         "whitespace trimmed",
			input:        "contacts: companies , deals ",
			wantBase:     "contacts",
			wantOverride: []string{"companies", "deals"},
			wantOK:       true,
		},
		{
			name:         "trailing comma",
			input:        "contacts:companies,deals,",
			wantBase:     "contacts",
			wantOverride: []string{"companies", "deals"},
			wantOK:       true,
		},
		{
			name:         "only commas",
			input:        "contacts:,,,",
			wantBase:     "contacts",
			wantOverride: []string{},
			wantOK:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBase, gotOverride, gotOK := parseTableAssocOverride(tc.input)
			if gotBase != tc.wantBase {
				t.Errorf("base: got %q, want %q", gotBase, tc.wantBase)
			}
			if !reflect.DeepEqual(gotOverride, tc.wantOverride) {
				t.Errorf("override: got %#v, want %#v", gotOverride, tc.wantOverride)
			}
			if gotOK != tc.wantOK {
				t.Errorf("ok: got %v, want %v", gotOK, tc.wantOK)
			}
		})
	}
}
