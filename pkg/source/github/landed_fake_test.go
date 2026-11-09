package github

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeRepo is an in-memory repository behind the landedAPI interface: compare, history and listings
// are computed from the commit graph, so scenarios only describe commits, PRs and pushes.
type fakeRepo struct {
	branch      string
	head        string
	liveHead    string
	pinnedAt    time.Time
	commits     map[string]*fakeCommit
	activityLog []activityEntry
	activityErr error
	suites      map[string][]checkSuite
	ciForbidden bool
	events      map[string][]eventPR
	branches    map[string]string
	compareCap  int
	aheadBias   int
	calls       map[string]int
}

type fakeCommit struct {
	oid     string
	parents []string
	date    time.Time
	signed  bool
	prs     []ghPR
}

var fakeT0 = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return fakeT0.Add(time.Duration(minutes) * time.Minute) }

func oid(name string) string {
	h := fmt.Sprintf("%x", name)
	return (h + strings.Repeat("0", 40))[:40]
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		branch:     "main",
		pinnedAt:   at(10000),
		commits:    map[string]*fakeCommit{},
		suites:     map[string][]checkSuite{},
		events:     map[string][]eventPR{},
		branches:   map[string]string{"main": "main"},
		compareCap: 10000,
		calls:      map[string]int{},
	}
}

func (f *fakeRepo) add(name string, minutes int, parents ...string) *fakeCommit {
	c := &fakeCommit{oid: oid(name), date: at(minutes)}
	for _, p := range parents {
		c.parents = append(c.parents, oid(p))
	}
	f.commits[c.oid] = c
	return c
}

func (f *fakeRepo) pr(number int, commit, head, mergeCommit string, mergedMinutes int, base string) ghPR {
	m := at(mergedMinutes)
	pr := ghPR{Number: number, MergedAt: &m, BaseRef: base, HeadOid: oid(head)}
	if mergeCommit != "" {
		pr.MergeCommit = oid(mergeCommit)
	}
	c := f.commits[oid(commit)]
	c.prs = append(c.prs, pr)
	return pr
}

func (f *fakeRepo) push(typ, before, after string, minutes int) {
	b := zeroSHA
	if before != "" {
		b = oid(before)
	}
	f.activityLog = append(f.activityLog, activityEntry{Before: b, After: oid(after), Timestamp: at(minutes), Type: typ, Ref: "refs/heads/" + f.branch})
}

func (f *fakeRepo) ancestors(o string) map[string]bool {
	seen := map[string]bool{}
	queue := []string{o}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		if seen[x] || f.commits[x] == nil {
			continue
		}
		seen[x] = true
		queue = append(queue, f.commits[x].parents...)
	}
	return seen
}

func (f *fakeRepo) byDate(set map[string]bool) []string {
	var out []string
	for o := range set {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := f.commits[out[i]], f.commits[out[j]]
		if !a.date.Equal(b.date) {
			return a.date.Before(b.date)
		}
		return out[i] < out[j]
	})
	return out
}

func (f *fakeRepo) raw(o string) map[string]interface{} {
	c := f.commits[o]
	var parents []interface{}
	for _, p := range c.parents {
		parents = append(parents, map[string]interface{}{"sha": p})
	}
	email := "dev@example.com"
	if c.signed {
		email = "noreply@github.com"
	}
	return map[string]interface{}{
		"sha":     o,
		"node_id": "C_" + o[:8],
		"url":     "https://api.github.com/repos/o/r/commits/" + o,
		"commit": map[string]interface{}{
			"message":      "commit " + o[:8],
			"author":       map[string]interface{}{"date": c.date.Add(-time.Hour).Format(time.RFC3339)},
			"committer":    map[string]interface{}{"date": c.date.Format(time.RFC3339), "email": email},
			"verification": map[string]interface{}{"verified": c.signed},
		},
		"parents": parents,
	}
}

func (f *fakeRepo) defaultBranch(context.Context) (string, error) { return f.branch, nil }

func (f *fakeRepo) branchHead(_ context.Context, branch string) (string, time.Time, bool, error) {
	f.calls["head"]++
	if f.head == "" {
		return "", time.Time{}, true, nil
	}
	if f.calls["head"] > 1 && f.liveHead != "" {
		return f.liveHead, f.pinnedAt, false, nil
	}
	return f.head, f.pinnedAt, false, nil
}

func (f *fakeRepo) commit(_ context.Context, o string) (*ghCommit, error) {
	c := f.commits[o]
	if c == nil {
		return nil, fmt.Errorf("commit %s not found", o)
	}
	f.calls["commit"]++
	gc := &ghCommit{Oid: o, CommittedDate: c.date, Parents: c.parents, SignedByGitHub: c.signed, PRs: c.prs, PRsTotal: len(c.prs)}
	return gc, nil
}

func (f *fakeRepo) pageMates(string) []*ghCommit { return nil }

func (f *fakeRepo) status(base, head string) string {
	if f.commits[base] == nil || f.commits[head] == nil {
		return "not_found"
	}
	if base == head {
		return "identical"
	}
	ah, ab := f.ancestors(head), f.ancestors(base)
	switch {
	case ah[base]:
		return "ahead"
	case ab[head]:
		return "behind"
	}
	for o := range ah {
		if ab[o] {
			return "diverged"
		}
	}
	return "not_found"
}

func (f *fakeRepo) compare(_ context.Context, base, head string, page int) (*compareResult, error) {
	f.calls["compare"]++
	st := f.status(base, head)
	if st == "not_found" {
		if f.commits[base] == nil || f.commits[head] == nil {
			return nil, fmt.Errorf("compare %s...%s failed with status 404", short(base), short(head))
		}
		return &compareResult{NoCommonAncestor: true}, nil
	}
	ah, ab := f.ancestors(head), f.ancestors(base)
	diff := map[string]bool{}
	for o := range ah {
		if !ab[o] {
			diff[o] = true
		}
	}
	list := f.byDate(diff)
	if len(list) > f.compareCap {
		list = list[len(list)-f.compareCap:]
	}
	lo := (page - 1) * comparePageSize
	hi := min(lo+comparePageSize, len(list))
	res := &compareResult{Status: st, AheadBy: len(diff) + f.aheadBias}
	for _, o := range list[min(lo, len(list)):hi] {
		res.Commits = append(res.Commits, f.raw(o))
	}
	return res, nil
}

func (f *fakeRepo) compareStatus(_ context.Context, base, head string) (string, error) {
	f.calls["status"]++
	return f.status(base, head), nil
}

func (f *fakeRepo) listCommits(_ context.Context, sha string, page int) ([]map[string]interface{}, error) {
	f.calls["list"]++
	list := f.byDate(f.ancestors(sha))
	lo := (page - 1) * comparePageSize
	hi := min(lo+comparePageSize, len(list))
	var out []map[string]interface{}
	for _, o := range list[min(lo, len(list)):hi] {
		out = append(out, f.raw(o))
	}
	return out, nil
}

func (f *fakeRepo) restCommit(_ context.Context, sha string) (map[string]interface{}, error) {
	return f.raw(sha), nil
}

func (f *fakeRepo) activity(_ context.Context, query url.Values, stop time.Time) ([]activityEntry, error) {
	if f.activityErr != nil {
		return nil, f.activityErr
	}
	var out []activityEntry
	for i := len(f.activityLog) - 1; i >= 0; i-- {
		e := f.activityLog[i]
		if ref := query.Get("ref"); ref != "" && e.Ref != ref {
			continue
		}
		if typ := query.Get("activity_type"); typ != "" && e.Type != typ {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}

func (f *fakeRepo) checkSuites(_ context.Context, o string) ([]checkSuite, error) {
	f.calls["ci"]++
	if f.ciForbidden {
		return nil, fmt.Errorf("%w: Resource not accessible by personal access token", errNoPermission)
	}
	return f.suites[o], nil
}

func (f *fakeRepo) mergedEvents(_ context.Context, reqs []eventRequest) (map[string][]eventPR, error) {
	f.calls["events"]++
	out := map[string][]eventPR{}
	for _, r := range reqs {
		out[r.Commit] = f.events[r.Commit]
	}
	return out, nil
}

func (f *fakeRepo) resolveBranch(_ context.Context, name string) (string, error) {
	return f.branches[name], nil
}

func (f *fakeRepo) mergedPullRequests(context.Context, time.Time) ([]ghPR, error) {
	f.calls["bulk"]++
	var out []ghPR
	for _, c := range f.commits {
		out = append(out, c.prs...)
	}
	return out, nil
}

type runResult struct {
	run      *landedRun
	rows     map[string]*landedRow
	order    []*landedRow
	warnings []string
	err      error
}

func runFake(t *testing.T, f *fakeRepo, start, end time.Time) runResult {
	t.Helper()
	var res runResult
	run := newLandedRun(f, f.branch, start, end, func(format string, args ...interface{}) {
		res.warnings = append(res.warnings, fmt.Sprintf(format, args...))
	})
	rows, err := run.collect(context.Background())
	res.run = run
	res.err = err
	res.order = rows
	res.rows = map[string]*landedRow{}
	for _, r := range rows {
		res.rows[r.sha] = r
	}
	return res
}

func (res runResult) row(t *testing.T, name string) *landedRow {
	t.Helper()
	r, ok := res.rows[oid(name)]
	if !ok {
		var got []string
		for _, x := range res.order {
			got = append(got, x.sha[:8])
		}
		t.Fatalf("row %s (%s) missing; rows: %v; warnings: %v", name, oid(name)[:8], got, res.warnings)
	}
	return r
}

func (res runResult) hasWarning(sub string) bool {
	for _, w := range res.warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func expectExact(t *testing.T, res runResult, name string, minutes int, source string) {
	t.Helper()
	r := res.row(t, name)
	want := at(minutes)
	if !r.exact || !r.landed.Equal(want) || r.min == nil || !r.min.Equal(want) || !r.max.Equal(want) || r.source != source {
		t.Fatalf("%s: got exact=%v landed=%s min=%s max=%s source=%s, want exact at %s from %s (warnings %v)",
			name, r.exact, r.landed.Format(time.RFC3339), fmtT(r.min), r.max.Format(time.RFC3339), r.source, want.Format(time.RFC3339), source, res.warnings)
	}
}

func expectWindow(t *testing.T, res runResult, name string, minMinutes *int, maxMinutes int, source string) {
	t.Helper()
	r := res.row(t, name)
	okMin := (minMinutes == nil && r.min == nil) || (minMinutes != nil && r.min != nil && r.min.Equal(at(*minMinutes)))
	if r.exact || !okMin || !r.max.Equal(at(maxMinutes)) || r.source != source {
		t.Fatalf("%s: got exact=%v min=%s max=%s source=%s, want window (%v, %s] from %s (warnings %v)",
			name, r.exact, fmtT(r.min), r.max.Format(time.RFC3339), r.source, minMinutes, at(maxMinutes).Format(time.RFC3339), source, res.warnings)
	}
	if r.min != nil && r.landed.Before(*r.min) || r.landed.After(r.max) {
		t.Fatalf("%s: estimate %s outside its window", name, r.landed.Format(time.RFC3339))
	}
}

func intp(v int) *int { return &v }
