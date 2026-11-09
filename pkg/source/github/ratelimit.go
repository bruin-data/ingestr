package github

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
)

// GitHub rate-limit handling, per
// https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#exceeding-the-rate-limit
// and https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api.
// REST rejects with 403/429; GraphQL's primary limit answers 200 with a RATE_LIMIT error.
const (
	maxRateLimitWait       = time.Hour
	secondaryRateLimitWait = time.Minute
	secondaryRateLimitText = "secondary rate limit"
	graphQLRateLimitType   = "RATE_LIMIT"
)

func retryOptions(owner, repo string) []ingestrhttp.Option {
	return []ingestrhttp.Option{
		ingestrhttp.WithRetry(ingestrhttp.DefaultRetryCount, ingestrhttp.DefaultRetryWait, maxRateLimitWait),
		ingestrhttp.WithRetryCondition(func(resp *ingestrhttp.Response, _ error) bool {
			return isRateLimited(resp)
		}),
		ingestrhttp.WithRetryStrategy(func(resp *ingestrhttp.Response, _ error) (time.Duration, error) {
			if wait, ok := rateLimitWait(resp, time.Now()); ok {
				output.Warnf("Warning: GitHub rate limit exceeded for %s/%s, waiting %v before retrying\n", owner, repo, wait.Round(time.Second))
				return wait, nil
			}
			return backoff(resp), nil
		}),
	}
}

func isRateLimited(resp *ingestrhttp.Response) bool {
	if resp == nil {
		return false
	}
	switch resp.StatusCode() {
	case http.StatusForbidden, http.StatusTooManyRequests:
		h := resp.Header()
		return h.Get("Retry-After") != "" ||
			h.Get("X-RateLimit-Remaining") == "0" ||
			strings.Contains(resp.String(), secondaryRateLimitText)
	case http.StatusOK:
		return hasGraphQLRateLimitError(resp.Body())
	}
	return false
}

func hasGraphQLRateLimitError(body []byte) bool {
	if !bytes.Contains(body, []byte(graphQLRateLimitType)) && !bytes.Contains(body, []byte(secondaryRateLimitText)) {
		return false
	}
	var parsed graphQLResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	for _, e := range parsed.Errors {
		if e.Type == graphQLRateLimitType || strings.Contains(e.Message, secondaryRateLimitText) {
			return true
		}
	}
	return false
}

// rateLimitWait returns how long GitHub asks us to wait: Retry-After, else until
// x-ratelimit-reset when the quota is spent, else one minute. Never zero, since
// resty treats a zero delay as "wait the maximum".
func rateLimitWait(resp *ingestrhttp.Response, now time.Time) (time.Duration, bool) {
	if !isRateLimited(resp) {
		return 0, false
	}
	h := resp.Header()
	wait := secondaryRateLimitWait
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
		wait = time.Duration(secs) * time.Second
	} else if h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			wait = time.Unix(reset, 0).Sub(now) + time.Second
		}
	}
	return min(max(wait, time.Second), maxRateLimitWait), true
}

func backoff(resp *ingestrhttp.Response) time.Duration {
	attempt := 1
	if resp != nil && resp.Attempt() > 0 {
		attempt = resp.Attempt()
	}
	return min(ingestrhttp.DefaultRetryWait<<(attempt-1), ingestrhttp.DefaultRetryMaxWait)
}
