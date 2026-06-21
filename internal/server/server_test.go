package server

import "testing"

func TestResolveRunRequestResolvesCredentialIDs(t *testing.T) {
	creds := NewCredentialsManager("")
	creds.data.Credentials["src"] = Credential{
		ConnectorID: "raw",
		Fields: map[string]string{
			"uri": "postgres://user:pass@localhost/db",
		},
	}
	creds.data.Credentials["dest"] = Credential{
		ConnectorID: "raw",
		Fields: map[string]string{
			"uri": "duckdb:///tmp/out.db",
		},
	}
	s := &Server{creds: creds}

	req := RunJobRequest{
		SourceCredentialID: "src",
		DestCredentialID:   "dest",
		SourceTable:        "public.users",
		Strategy:           "merge",
	}
	if err := s.resolveRunRequest(&req); err != nil {
		t.Fatalf("resolveRunRequest: %v", err)
	}
	if req.SourceURI != "postgres://user:pass@localhost/db" {
		t.Fatalf("source URI = %q", req.SourceURI)
	}
	if req.DestURI != "duckdb:///tmp/out.db" {
		t.Fatalf("dest URI = %q", req.DestURI)
	}
	if req.DestTable != "public.users" {
		t.Fatalf("dest table = %q, want source table default", req.DestTable)
	}
	if req.IncrementalStrategy != "merge" {
		t.Fatalf("incremental strategy = %q, want legacy strategy value", req.IncrementalStrategy)
	}
	if req.Progress != "log" {
		t.Fatalf("progress = %q, want log default", req.Progress)
	}
}

func TestResolveRunRequestRejectsMissingCredential(t *testing.T) {
	s := &Server{creds: NewCredentialsManager("")}
	req := RunJobRequest{SourceCredentialID: "missing", DestURI: "duckdb:///tmp/out.db"}
	if err := s.resolveRunRequest(&req); err == nil {
		t.Fatal("expected missing source credential error")
	}
}
