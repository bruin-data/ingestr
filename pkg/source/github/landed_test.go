package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Scenario tests on the in-memory repository. Times are minutes after 2026-11-01T00:00Z; the
// interval is [100, 200) and the run pins the head at minute 10000 unless a test says otherwise.

const (
	iStart = 100
	iEnd   = 200
)

// baseRepo: root R and M0, a GitHub button merge of PR #10 into main at minute 50 (before the
// interval), so every walk stops at M0.
func baseRepo() *fakeRepo {
	f := newFakeRepo()
	f.add("R", 0)
	f.add("X0", 40, "R")
	m0 := f.add("M0", 50, "R", "X0")
	m0.signed = true
	f.pr(10, "M0", "X0", "M0", 50, "main")
	return f
}

func noActivity(f *fakeRepo) *fakeRepo {
	f.activityErr = errors.New("status 403: Resource not accessible")
	return f
}

func TestLandedButtonMergesAreExact(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("X1", -5000, "M0")
	m1 := f.add("M1", 120, "M0", "X1")
	m1.signed = true
	f.pr(1, "M1", "X1", "M1", 120, "main")
	f.add("X2", 130, "M0")
	m2 := f.add("M2", 150, "M1", "X2")
	m2.signed = true
	f.pr(2, "M2", "X2", "M2", 150, "main")
	f.head = oid("M2")

	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if len(res.rows) != 4 {
		t.Fatalf("rows=%d want 4 (X1, M1, X2, M2)", len(res.rows))
	}
	expectExact(t, res, "X1", 120, srcPR)
	expectExact(t, res, "M1", 120, srcPR)
	expectExact(t, res, "X2", 150, srcPR)
	expectExact(t, res, "M2", 150, srcPR)
	if r := res.row(t, "X1"); r.pr != 1 || r.step != oid("M1") {
		t.Fatalf("X1 pr=%d via=%s, want PR 1 via M1", r.pr, short(r.step))
	}
	if r := res.row(t, "X1"); r.committedAt == nil || !r.committedAt.Equal(at(-5000)) || r.authoredAt == nil {
		t.Fatalf("X1 committed_at=%v authored_at=%v", r.committedAt, r.authoredAt)
	}
	if !res.hasWarning("activity log unavailable") || !res.hasWarning("removed from main by force pushes cannot be marked") {
		t.Fatalf("warnings=%v, want the activity warnings", res.warnings)
	}
	if res.hasWarning("conflicting landing times") {
		t.Fatalf("unexpected invariant violation: %v", res.warnings)
	}
}

func directPushRepo() *fakeRepo {
	f := baseRepo()
	f.add("D1", -100, "M0")
	f.add("D2", -90, "D1")
	f.add("D3", -80, "D2")
	f.head = oid("D3")
	return f
}

func TestLandedDirectPushesWithoutAnyServerTimeGetWindows(t *testing.T) {
	f := noActivity(directPushRepo())
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	for _, name := range []string{"D1", "D2", "D3"} {
		expectWindow(t, res, name, intp(50), 10000, srcClient)
		if r := res.row(t, name); !r.landed.Equal(at(50)) {
			t.Fatalf("%s estimate=%s, want its commit date clamped to the window start", name, r.landed.Format(time.RFC3339))
		}
	}
	if f.calls["ci"] != 3 {
		t.Fatalf("check-suite lookups=%d want 3 (one per untrusted step)", f.calls["ci"])
	}
}

func TestLandedActivityPushRangeMakesEveryCommitExact(t *testing.T) {
	f := directPushRepo()
	f.push("pr_merge", "R", "M0", 50)
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "D2", 120)
	f.push("push", "D2", "D3", 160)
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "D1", 120, srcActivity)
	expectExact(t, res, "D2", 120, srcActivity)
	expectExact(t, res, "D3", 160, srcActivity)
	if res.hasWarning("conflicting landing times") {
		t.Fatalf("warnings=%v", res.warnings)
	}
	if res.run.relanded[oid("M0")] {
		t.Fatal("a duplicated activity entry must not count as a second landing")
	}
}

func TestLandedCIRecordAndMissingPermission(t *testing.T) {
	f := noActivity(directPushRepo())
	f.suites[oid("D2")] = []checkSuite{
		{CreatedAt: at(125), Branch: "feature", HasPush: true, PreviousSha: oid("D1")},
		{CreatedAt: at(121), Branch: "main", HasPush: true, PreviousSha: oid("M0")},
		{CreatedAt: at(122), Branch: "main", HasPush: true, PreviousSha: oid("M0")},
	}
	f.suites[oid("D3")] = []checkSuite{{CreatedAt: at(161), Branch: "main", HasPush: true, PreviousSha: oid("D2")}}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "D1", 121, srcCI)
	expectExact(t, res, "D2", 121, srcCI)
	expectExact(t, res, "D3", 161, srcCI)

	f2 := noActivity(directPushRepo())
	f2.ciForbidden = true
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.err != nil {
		t.Fatal(res2.err)
	}
	if !res2.hasWarning("CI records are not used") {
		t.Fatalf("warnings=%v, want the CI permission warning", res2.warnings)
	}
	expectWindow(t, res2, "D3", intp(50), 10000, srcClient)
	if f2.calls["ci"] != 1 {
		t.Fatalf("check-suite lookups=%d, want 1 before CI is turned off", f2.calls["ci"])
	}
}

func TestLandedOldDatedCommitsPushedRecentlyAreAskedForCI(t *testing.T) {
	f := noActivity(directPushRepo())
	f.pinnedAt = at(500 * 24 * 60)
	f.suites[oid("D3")] = []checkSuite{{CreatedAt: at(500*24*60 - 5), Branch: "main", HasPush: true, PreviousSha: oid("M0")}}
	res := runFake(t, f, at(500*24*60-60), at(500*24*60))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if f.calls["ci"] != 3 {
		t.Fatalf("check-suite lookups=%d, want 3: the landings are bounded by recent times, not by the old commit dates", f.calls["ci"])
	}
	for _, name := range []string{"D1", "D2", "D3"} {
		expectExact(t, res, name, 500*24*60-5, srcCI)
	}
	if res.hasWarning("conflicting landing times") {
		t.Fatalf("unexpected invariant violation: %v", res.warnings)
	}
}

// ciCutRepo: the direct pushes D1..D3 sit below M2, a button merge at minute 150, and the run pins
// the head pinnedDays later, so M2's time bounds every landing below it.
func ciCutRepo(pinnedDays int) *fakeRepo {
	f := noActivity(directPushRepo())
	f.add("X2", 140, "M0")
	m2 := f.add("M2", 150, "D3", "X2")
	m2.signed = true
	f.pr(2, "M2", "X2", "M2", 150, "main")
	f.head = oid("M2")
	f.pinnedAt = at(150 + pinnedDays*24*60)
	return f
}

func TestLandedCIIsSkippedWhenTheLandingBoundIsPastTheRetention(t *testing.T) {
	f := ciCutRepo(411)
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if f.calls["ci"] != 0 {
		t.Fatalf("check-suite lookups=%d, want none: everything below M2 landed by minute 150, more than 410 days ago", f.calls["ci"])
	}
	expectWindow(t, res, "D3", intp(50), 150, srcClient)

	f2 := ciCutRepo(409)
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.err != nil {
		t.Fatal(res2.err)
	}
	if f2.calls["ci"] == 0 {
		t.Fatal("within 410 days of the landing bound the check suites must still be asked")
	}
}

func pushDetectedRepo() *fakeRepo {
	f := baseRepo()
	f.add("P1", -10, "M0")
	f.add("P2", -5, "P1")
	f.pr(3, "P2", "P2", "P2", 130, "main")
	f.pr(3, "P1", "P2", "P2", 130, "main")
	f.head = oid("P2")
	return f
}

func TestLandedPushDetectedPRIsAnUpperBound(t *testing.T) {
	f := noActivity(pushDetectedRepo())
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectWindow(t, res, "P1", intp(50), 130, srcClient)
	expectWindow(t, res, "P2", intp(50), 130, srcPR+windowSuffix)
	if res.row(t, "P2").pr != 3 {
		t.Fatalf("P2 pr=%d want 3", res.row(t, "P2").pr)
	}

	f2 := pushDetectedRepo()
	f2.push("pr_merge", "R", "M0", 50)
	f2.push("push", "M0", "P2", 125)
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.err != nil {
		t.Fatal(res2.err)
	}
	expectExact(t, res2, "P1", 125, srcActivity)
	expectExact(t, res2, "P2", 125, srcActivity)
}

func TestLandedLaterPushCoversAnUpperBoundStep(t *testing.T) {
	f := baseRepo()
	f.add("P1", -10, "M0")
	f.add("P2", -5, "P1")
	f.pr(3, "P2", "P2", "P2", 151, "main")
	f.add("P3", -1, "P2")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "P3", 150)
	f.head = oid("P3")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	for _, name := range []string{"P1", "P2", "P3"} {
		expectExact(t, res, name, 150, srcActivity)
	}
}

func TestLandedPRStepWithActivityPushRange(t *testing.T) {
	f := baseRepo()
	f.add("L1", -30, "M0")
	f.add("L2", -20, "L1")
	f.pr(4, "L2", "L1x", "L2", 131, "main")
	f.add("L1x", -25, "M0")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "L2", 130)
	f.head = oid("L2")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "L2", 131, srcPR)
	expectExact(t, res, "L1", 130, srcActivity)
}

func TestLandedResetThenRestore(t *testing.T) {
	f := baseRepo()
	f.add("B1", 105, "M0")
	f.add("C1", 106, "B1")
	f.add("D1", 129, "M0")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "C1", 110)
	f.push("force_push", "C1", "M0", 120)
	f.push("push", "M0", "D1", 130)
	f.push("force_push", "D1", "C1", 140)
	f.head = oid("C1")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "C1", 140, srcActivity)
	expectExact(t, res, "B1", 140, srcActivity)
	d1 := res.row(t, "D1")
	if d1.removedAt == nil || !d1.removedAt.Equal(at(140)) {
		t.Fatalf("D1 removed_from_branch_at=%v want minute 140", d1.removedAt)
	}
	expectExact(t, res, "D1", 130, srcActivity)
	if res.row(t, "C1").removedAt != nil || res.row(t, "B1").removedAt != nil {
		t.Fatal("restored commits must not be marked removed")
	}
	if _, ok := res.rows[oid("M0")]; ok {
		t.Fatal("M0 (reset target, landed before the interval) must not be re-landed")
	}
}

func TestLandedRemovedCommitWithoutActivityLanding(t *testing.T) {
	f := baseRepo()
	f.add("U1", 101, "M0")
	f.push("pr_merge", "R", "M0", 50)
	f.push("force_push", "U1", "M0", 150)
	f.head = oid("M0")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	u1 := res.row(t, "U1")
	if u1.removedAt == nil || !u1.removedAt.Equal(at(150)) {
		t.Fatalf("U1 removed_from_branch_at=%v want minute 150", u1.removedAt)
	}
	expectWindow(t, res, "U1", nil, 150, srcClient+windowSuffix)
	if !u1.landed.Equal(at(101)) {
		t.Fatalf("U1 estimate=%s want its commit date", u1.landed.Format(time.RFC3339))
	}
}

func sideBranchMerge(f *fakeRepo, n int) {
	prev := "R"
	for i := 1; i <= n; i++ {
		name := "S" + string(rune('a'+i))
		f.add(name, i, prev)
		prev = name
	}
	m1 := f.add("M1", 120, "M0", prev)
	m1.signed = true
	f.pr(5, "M1", prev, "M1", 120, "main")
	f.head = oid("M1")
}

func TestLandedTruncatedCompareIsSplitAndCounted(t *testing.T) {
	f := noActivity(baseRepo())
	sideBranchMerge(f, 7)
	f.compareCap = 3
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if len(res.rows) != 8 {
		t.Fatalf("rows=%d want 8 (7 side commits + M1)", len(res.rows))
	}
	for _, r := range res.order {
		if !r.exact || !r.landed.Equal(at(120)) {
			t.Fatalf("%s exact=%v landed=%s, want exact at 120", short(r.sha), r.exact, r.landed.Format(time.RFC3339))
		}
	}

	f2 := noActivity(baseRepo())
	sideBranchMerge(f2, 7)
	f2.compareCap = 3
	f2.aheadBias = 1
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.err == nil || len(res2.rows) != 0 {
		t.Fatalf("err=%v rows=%d, want a Q6 set violation and nothing written", res2.err, len(res2.rows))
	}
}

func TestLandedUnrelatedHistoryFallsBackToListing(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("U1", 5)
	f.add("U2", 6, "U1")
	m1 := f.add("M1", 120, "M0", "U2")
	m1.signed = true
	f.pr(6, "M1", "U2", "M1", 120, "main")
	f.head = oid("M1")
	f.compareCap = 2
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	for _, name := range []string{"U1", "U2", "M1"} {
		expectExact(t, res, name, 120, srcPR)
	}
	if f.calls["list"] != 1 {
		t.Fatalf("commits?sha= listings=%d want 1", f.calls["list"])
	}
}

func TestLandedRenamedBranchReadsTheOldNamesActivity(t *testing.T) {
	f := newFakeRepo()
	f.add("R", 0)
	f.add("X0", 40, "R")
	f.add("M0", 50, "R", "X0").signed = true
	f.pr(10, "M0", "X0", "M0", 50, "trunk")
	f.add("T1", -50, "M0")
	f.add("T2", -40, "T1")
	f.head = oid("T2")
	f.activityLog = []activityEntry{
		{Before: oid("M0"), After: oid("T1"), Timestamp: at(110), Type: "push", Ref: "refs/heads/trunk"},
		{Before: zeroSHA, After: oid("T1"), Timestamp: at(115), Type: "branch_creation", Ref: "refs/heads/main"},
		{Before: oid("T1"), After: zeroSHA, Timestamp: at(115).Add(time.Second), Type: "branch_deletion", Ref: "refs/heads/trunk"},
		{Before: oid("T1"), After: oid("T2"), Timestamp: at(150), Type: "push", Ref: "refs/heads/main"},
	}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "T1", 110, srcActivity)
	expectExact(t, res, "T2", 150, srcActivity)
}

func TestLandedPRBaseAliasResolvesToTheDefaultBranch(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("X1", 110, "M0")
	m1 := f.add("M1", 120, "M0", "X1")
	m1.signed = true
	f.pr(7, "M1", "X1", "M1", 120, "master")
	f.branches["master"] = "main"
	f.head = oid("M1")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "M1", 120, srcPR)
	expectExact(t, res, "X1", 120, srcPR)
}

func preMergeCommitRepo(conflict bool) *fakeRepo {
	f := newFakeRepo()
	f.activityErr = errors.New("unavailable")
	f.add("R", 0)
	f.add("X0", 40, "R")
	f.add("M0", 50, "R", "X0")
	m := at(50)
	f.events[oid("M0")] = []eventPR{{ghPR: ghPR{Number: 20, MergedAt: &m, BaseRef: "main", HeadOid: oid("X0"), EventCommits: []string{oid("M0")}}, FromParent2: true}}
	f.add("X1", 30, "R")
	f.add("M1", 120, "M0", "X1")
	m1 := at(120)
	pr := ghPR{Number: 21, MergedAt: &m1, BaseRef: "main", HeadOid: oid("X1"), EventCommits: []string{oid("M1")}}
	if conflict {
		pr.MergeCommit = oid("M0")
	}
	f.events[oid("M1")] = []eventPR{{ghPR: pr, FromParent2: true}}
	f.head = oid("M1")
	return f
}

func TestLandedMergedEventMatchesOldMerges(t *testing.T) {
	f := preMergeCommitRepo(false)
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "M1", 120, srcPR)
	expectWindow(t, res, "X1", intp(50), 120, srcPR+windowSuffix)
	if res.row(t, "X1").pr != 21 {
		t.Fatalf("X1 pr=%d want 21", res.row(t, "X1").pr)
	}
}

func TestLandedConflictingMergeCommitAndEventTrustNeither(t *testing.T) {
	f := preMergeCommitRepo(true)
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.hasWarning("PR #21 has a merge commit") {
		t.Fatalf("warnings=%v, want the conflict warning", res.warnings)
	}
	if res.row(t, "M1").exact {
		t.Fatal("M1 must not be exact when its PR's mergeCommit and MergedEvent disagree")
	}
}

func TestLandedRuleCLocalMergeDetectedFromPush(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("H", -20, "M0")
	f.pr(30, "H", "H", "H", 130, "main")
	f.add("M1", 125, "M0", "H")
	m := at(130)
	f.events[oid("M1")] = []eventPR{{ghPR: ghPR{Number: 30, MergedAt: &m, BaseRef: "main", HeadOid: oid("H"), MergeCommit: oid("H"), EventCommits: []string{oid("H")}}, FromParent2: true}}
	f.head = oid("M1")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectWindow(t, res, "M1", intp(50), 130, srcPR+windowSuffix)
	expectWindow(t, res, "H", intp(50), 130, srcPR+windowSuffix)
	if res.row(t, "H").pr != 30 {
		t.Fatalf("H pr=%d want 30", res.row(t, "H").pr)
	}
}

func TestLandedHeadRewrittenDuringTheRunWarns(t *testing.T) {
	f := noActivity(directPushRepo())
	f.add("Z", 300, "M0")
	f.liveHead = oid("Z")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.hasWarning("main was rewritten during the run") {
		t.Fatalf("warnings=%v, want the Q3a warning", res.warnings)
	}

	f2 := noActivity(directPushRepo())
	f2.add("Z", 300, "D3")
	f2.liveHead = oid("Z")
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.hasWarning("rewritten") {
		t.Fatalf("a head that only moved forward must not warn: %v", res2.warnings)
	}
}

func TestLandedTimeViolationWidensInsteadOfFailing(t *testing.T) {
	f := baseRepo()
	f.add("D1", 10, "M0")
	f.add("D2", 11, "D1")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "D1", 160)
	f.push("push", "D1", "D2", 140)
	f.head = oid("D2")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.hasWarning("trusted landing times go backwards") || !res.hasWarning("the activity log says") {
		t.Fatalf("warnings=%v, want an I4 violation and an I9 count warning", res.warnings)
	}
	for _, name := range []string{"D1", "D2"} {
		r := res.row(t, name)
		if r.exact || !slices.Contains(r.widened, "I4") {
			t.Fatalf("%s exact=%v widened=%v, want a widened window", name, r.exact, r.widened)
		}
		if r.min == nil || r.min.After(at(140)) || r.max.Before(at(160)) {
			t.Fatalf("%s window (%s, %s] must hold both conflicting times", name, fmtT(r.min), r.max.Format(time.RFC3339))
		}
	}
}

func TestLandedPRListOverflowUsesTheBulkList(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("X1", 110, "M0")
	m1 := f.add("M1", 120, "M0", "X1")
	m1.signed = true
	f.pr(8, "M1", "X1", "M1", 120, "main")
	f.head = oid("M1")
	api := &overflowAPI{fakeRepo: f}
	run := newLandedRun(api, "main", at(iStart), at(iEnd), func(string, ...interface{}) {})
	rows, err := run.collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if f.calls["bulk"] != 1 {
		t.Fatalf("bulk PR listings=%d want 1", f.calls["bulk"])
	}
	found := false
	for _, r := range rows {
		if r.sha == oid("M1") {
			found = r.exact && r.landed.Equal(at(120)) && r.pr == 8
		}
	}
	if !found {
		t.Fatal("M1 must be trusted through the bulk PR list")
	}
}

type overflowAPI struct{ *fakeRepo }

func (o *overflowAPI) commit(ctx context.Context, oidStr string) (*ghCommit, error) {
	c, err := o.fakeRepo.commit(ctx, oidStr)
	if err != nil || oidStr != oid("M1") {
		return c, err
	}
	hidden := *c
	hidden.PRs = nil
	hidden.PRsTotal = 11
	return &hidden, nil
}

func buttonMergeRepo() *fakeRepo {
	f := baseRepo()
	f.add("X1", 110, "M0")
	m1 := f.add("M1", 120, "M0", "X1")
	m1.signed = true
	f.pr(1, "M1", "X1", "M1", 120, "main")
	f.push("pr_merge", "R", "M0", 50)
	f.push("pr_merge", "M0", "M1", 120)
	f.head = oid("M1")
	return f
}

func TestLandedSetChecksPassOnConsistentData(t *testing.T) {
	f := buttonMergeRepo()
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.hasWarning("was merged") || res.hasWarning("the activity log says") {
		t.Fatalf("unexpected set-check warning: %v", res.warnings)
	}
	if f.calls["bulk"] != 1 {
		t.Fatalf("bulk PR listings=%d, want 1 for I6", f.calls["bulk"])
	}
	if f.calls["count"] != 1 {
		t.Fatalf("compare counts=%d, want 1 for I9 (head M0 at the start, M1 at the end)", f.calls["count"])
	}
}

func TestLandedI6WarnsAndStillWritesRows(t *testing.T) {
	f := buttonMergeRepo()
	late, inside := at(150), at(130)
	f.extraBulk = []ghPR{
		{Number: 14, MergedAt: &inside, BaseRef: "main", HeadOid: oid("H14"), MergeCommit: oid("M0")},
		{Number: 99, MergedAt: &late, BaseRef: "main", HeadOid: oid("H99"), MergeCommit: oid("M1")},
		{Number: 98, MergedAt: &late, BaseRef: "release", HeadOid: oid("H98"), MergeCommit: oid("M0")},
	}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatalf("I6 must not fail the run: %v", res.err)
	}
	if !res.hasWarning("PR #14 was merged into main at 2026-11-01T02:10:00Z, its commit " + short(oid("M0")) + " is on main but missing from the output") {
		t.Fatalf("warnings=%v, want the PR #14 warning", res.warnings)
	}
	if !res.hasWarning("PR #99 was merged at 2026-11-01T02:30:00Z, but its commit " + short(oid("M1"))) {
		t.Fatalf("warnings=%v, want the PR #99 window warning", res.warnings)
	}
	if res.hasWarning("PR #98") || res.hasWarning("PR #1 ") {
		t.Fatalf("PRs into other branches and consistent PRs must not warn: %v", res.warnings)
	}
	expectExact(t, res, "M1", 120, srcPR)
	expectExact(t, res, "X1", 120, srcPR)
}

func TestLandedI6ChecksOnlyTheUpperBoundForPushDetectedPRs(t *testing.T) {
	f := noActivity(pushDetectedRepo())
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.hasWarning("was merged") {
		t.Fatalf("a push-detected PR with mergedAt at the window max must not warn: %v", res.warnings)
	}

	f2 := noActivity(pushDetectedRepo())
	early := at(40)
	f2.extraBulk = []ghPR{{Number: 77, MergedAt: &early, BaseRef: "main", HeadOid: oid("P2"), MergeCommit: oid("P2")}}
	res2 := runFake(t, f2, at(30), at(iEnd))
	if !res2.hasWarning("PR #77 was merged") || !res2.hasWarning("only the window start is checked") {
		t.Fatalf("warnings=%v, want the push-detected PR #77 below the window min", res2.warnings)
	}
}

func TestLandedCIRecordUnderAFormerBranchName(t *testing.T) {
	f := noActivity(directPushRepo())
	f.branches["master"] = "main"
	f.suites[oid("D3")] = []checkSuite{
		{CreatedAt: at(125), Branch: "feature", HasPush: true, PreviousSha: oid("M0")},
		{CreatedAt: at(161), Branch: "master", HasPush: true, PreviousSha: oid("M0")},
	}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	for _, name := range []string{"D1", "D2", "D3"} {
		expectExact(t, res, name, 161, srcCI)
	}
}

func TestLandedOneRowPerCommit(t *testing.T) {
	var warnings []string
	r := newLandedRun(newFakeRepo(), "main", at(iStart), at(iEnd), func(f string, a ...interface{}) { warnings = append(warnings, fmt.Sprintf(f, a...)) })
	gone := at(150)
	onB := &landedRow{sha: oid("A"), step: oid("S1"), landed: at(120), max: at(120)}
	removed := &landedRow{sha: oid("A"), step: oid("S2"), landed: at(110), max: at(110), removedAt: &gone}
	other := &landedRow{sha: oid("B"), step: oid("S1"), landed: at(120), max: at(120)}
	out := r.oneRowPerCommit([]*landedRow{removed, other, onB})
	if len(out) != 2 || out[0] != onB || out[1] != other {
		t.Fatalf("rows=%d, want 2 with the on-branch row of A kept in place", len(out))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "commit "+short(oid("A"))+" produced two rows") || !strings.Contains(warnings[0], "keeping the one landed via "+short(oid("S1"))) {
		t.Fatalf("warnings=%v", warnings)
	}
}

func TestLandedRootCommitWithAServerTimeIsWritten(t *testing.T) {
	f := noActivity(newFakeRepo())
	f.add("R", 0)
	f.suites[oid("R")] = []checkSuite{{CreatedAt: at(151), Branch: "main", HasPush: true, PreviousSha: zeroSHA}}
	f.head = oid("R")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if len(res.rows) != 1 {
		t.Fatalf("rows=%d want 1 (the root)", len(res.rows))
	}
	expectExact(t, res, "R", 151, srcCI)

	f2 := noActivity(newFakeRepo())
	f2.add("R", 0)
	f2.add("A", 1, "R")
	f2.suites[oid("R")] = []checkSuite{{CreatedAt: at(121), Branch: "main", HasPush: true, PreviousSha: zeroSHA}}
	f2.suites[oid("A")] = []checkSuite{{CreatedAt: at(151), Branch: "main", HasPush: true, PreviousSha: oid("R")}}
	f2.head = oid("A")
	res2 := runFake(t, f2, at(iStart), at(iEnd))
	if res2.err != nil {
		t.Fatal(res2.err)
	}
	expectExact(t, res2, "R", 121, srcCI)
	expectExact(t, res2, "A", 151, srcCI)

	res3 := runFake(t, f2, at(130), at(iEnd))
	if res3.err != nil {
		t.Fatal(res3.err)
	}
	if _, ok := res3.rows[oid("R")]; ok {
		t.Fatal("a root that landed before the interval must not be returned")
	}
}

func TestLandedEventPRListOverflowUsesTheBulkList(t *testing.T) {
	f := preMergeCommitRepo(false)
	pr := f.events[oid("M1")][0].ghPR
	delete(f.events, oid("M1"))
	f.evOverflow = map[string]bool{oid("M1"): true}
	f.extraBulk = []ghPR{pr}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectExact(t, res, "M1", 120, srcPR)
	if res.row(t, "M1").pr != 21 {
		t.Fatalf("M1 pr=%d want 21 (found through the bulk list)", res.row(t, "M1").pr)
	}
}

func TestLandedForcePushLandingNeverMovesARowBelowItsWindow(t *testing.T) {
	f := baseRepo()
	f.add("K", 60, "M0")
	f.add("C", 61, "M0")
	f.add("Z", 62, "M0")
	f.add("P", 150, "K", "C")
	f.pr(5, "P", "C", "P", 150, "main")
	f.push("pr_merge", "R", "M0", 50)
	f.push("force_push", "Z", "C", 110)
	f.push("push", "M0", "K", 130)
	f.head = oid("P")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	expectWindow(t, res, "C", intp(130), 150, srcPR+windowSuffix)
}

// A conflict found after the PR's mergeCommit was already trusted: X (newer, mergeCommit of PR #22)
// is walked first, Y (older, the PR's MergedEvent commit) later. Neither may give a landing time.
func TestLandedConflictWithdrawsTrustGivenEarlier(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("H", 105, "M0")
	f.add("Y", 110, "M0", "H")
	x := f.add("X", 111, "Y")
	x.signed = true
	merged := at(110)
	pr := ghPR{Number: 22, MergedAt: &merged, BaseRef: "main", HeadOid: oid("H"), MergeCommit: oid("X"), EventCommits: []string{oid("Y")}}
	x.prs = append(x.prs, pr)
	f.events[oid("Y")] = []eventPR{{ghPR: pr, FromParent2: true}}
	f.head = oid("X")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.hasWarning("PR #22 has a merge commit") {
		t.Fatalf("warnings=%v, want the conflict warning", res.warnings)
	}
	for _, name := range []string{"X", "Y", "H"} {
		r := res.row(t, name)
		if r.exact || r.pr == 22 {
			t.Fatalf("%s exact=%v pr=%d source=%s: PR #22 must not give a landing time", name, r.exact, r.pr, r.source)
		}
	}
	if f.calls["events"] != 1 {
		t.Fatalf("event queries=%d, want 1: walking again must not ask for events again", f.calls["events"])
	}
}
