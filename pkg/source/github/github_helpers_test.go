package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
)

func TestExecuteGraphQLReturnsTypedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repository":null},"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by personal access token"}]}`))
	}))
	defer srv.Close()
	s := &GithubSource{owner: "o", repo: "r", graphqlClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL))}

	_, _, err := s.executeGraphQL(context.Background(), "query { x }", nil)
	gqlErrs, ok := err.(graphQLErrors)
	if !ok || !gqlErrs.hasType("FORBIDDEN") || gqlErrs.hasType("NOT_FOUND") {
		t.Fatalf("err=%v (%T), want graphQLErrors with type FORBIDDEN", err, err)
	}
	if err.Error() != "graphql errors: Resource not accessible by personal access token" {
		t.Fatalf("message=%q", err.Error())
	}
}

func TestRestGetReturnsOtherAnswersToTheCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	s := &GithubSource{owner: "o", repo: "r", restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL))}

	var body map[string]interface{}
	resp, err := s.restGet(context.Background(), "/repos/o/r/branches/x", &body)
	if err != nil || resp.StatusCode() != http.StatusNotFound {
		t.Fatalf("resp=%v err=%v, want the 404 back", resp, err)
	}
}
