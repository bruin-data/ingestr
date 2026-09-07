package hubspot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

// TestFetchAssociationsBatchNumericID verifies that a numeric toObjectId (as the
// v4 batch/read endpoint returns it) is emitted as a plain id string rather than
// float scientific notation like "4.46642248919e+11".
func TestFetchAssociationsBatchNumericID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// toObjectId is a JSON number, matching HubSpot's actual response.
		_, _ = w.Write([]byte(`{"results":[{"from":{"id":"862245463262"},"to":[{"toObjectId":446642248919}]}]}`))
	}))
	defer srv.Close()

	s := &Hubspotsource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
	got, err := s.fetchAssociationsBatch(context.Background(), "contacts", "companies", []string{"862245463262"})
	if err != nil {
		t.Fatal(err)
	}

	ids := got["862245463262"]
	if len(ids) != 1 || ids[0] != "446642248919" {
		t.Fatalf("expected [446642248919], got %#v", ids)
	}
}
