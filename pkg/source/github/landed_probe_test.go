package github

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func strictlyTrusted(res runResult, name string) bool {
	_, ok := res.run.trusts[1][oid(name)]
	return ok
}

// meetRepo: merge MX (no PR, no activity) of A1 and A2, two lines that meet at C before reaching M0.
func meetRepo() *fakeRepo {
	f := noActivity(baseRepo())
	f.add("C", 60, "M0")
	f.add("A1", 61, "C")
	f.add("A2", 62, "C")
	f.add("MX", 120, "A1", "A2")
	f.head = oid("MX")
	return f
}

func TestProbeStopsWhereTheParentsMeet(t *testing.T) {
	f := meetRepo()
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if strictlyTrusted(res, "M0") {
		t.Fatal("the probes met at C; nothing below C may be evaluated by the probe")
	}
	if got := res.run.probeSkip[oid("A2")]; got != oid("M0") {
		t.Fatalf("stop point cached for A2 = %s, want M0 (where the joined probe stopped)", short(got))
	}
	if r := res.row(t, "A2"); r.step != oid("MX") {
		t.Fatalf("A2 via %s, want via MX (parent 1 followed)", short(r.step))
	}
}

// ancestorRepo: merge MX of P1 (trusted at once by an activity push) and S3, a line S3 -> S2 -> S1
// that joins P1's history at Q.
func ancestorRepo() *fakeRepo {
	f := baseRepo()
	f.add("Q", 60, "M0")
	f.add("P1", 61, "Q")
	f.add("S1", 62, "Q")
	f.add("S2", 63, "S1")
	f.add("S3", 64, "S2")
	f.add("MX", 120, "P1", "S3")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "M0", "P1", 110)
	f.head = oid("MX")
	return f
}

func TestProbeStopsAtAnAncestorOfTheOtherParentsTrustedState(t *testing.T) {
	f := ancestorRepo()
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	for _, name := range []string{"S3", "S2", "S1"} {
		if !strictlyTrusted(res, name) {
			t.Fatalf("%s is P1-side-only and must be probed", name)
		}
	}
	if strictlyTrusted(res, "Q") {
		t.Fatal("Q is an ancestor of P1's trusted state; the probe must stop there")
	}
	if r := res.row(t, "S3"); r.step != oid("MX") {
		t.Fatalf("S3 via %s, want MX: parent 1 (P1) wins", short(r.step))
	}
}

func TestProbeKeepsWalkingWhenTheAncestorCompareIsTruncated(t *testing.T) {
	f := ancestorRepo()
	f.compareCap = 2
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.hasWarning("more commits than GitHub returns") {
		t.Fatalf("warnings=%v, want the truncated-compare warning", res.warnings)
	}
	if !strictlyTrusted(res, "Q") {
		t.Fatal("without a usable ahead set the probe must keep walking past Q")
	}
	if r := res.row(t, "S3"); r.step != oid("MX") {
		t.Fatalf("S3 via %s, want MX", short(r.step))
	}
}

func TestProbeHasNoBudget(t *testing.T) {
	f := baseRepo()
	f.add("T", 30, "R")
	f.push("pr_merge", "R", "M0", 50)
	f.push("push", "R", "T", 90)
	prev := "T"
	for i := 0; i < 250; i++ {
		name := fmt.Sprintf("L%03d", i)
		f.add(name, 31, prev)
		prev = name
	}
	f.add("P1", 70, "M0")
	f.add("MX", 120, "P1", prev)
	f.head = oid("MX")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	onChain := false
	for _, st := range res.run.steps {
		if st.oid == oid("T") {
			onChain = true
		}
	}
	if !onChain {
		t.Fatal("parent 2 reaches T (90) after 250 commits, newer than parent 1's M0 (50): the walk must follow parent 2")
	}
}

func TestProbeAPIErrorFailsTheRun(t *testing.T) {
	f := ancestorRepo()
	f.failOn = map[string]error{oid("S2"): errors.New("API rate limit exceeded")}
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err == nil || !strings.Contains(res.err.Error(), "rate limit") {
		t.Fatalf("err=%v, want the API error instead of a silent parent 1", res.err)
	}
}

// floorRepo: no merged PRs. The activity log starts more than 410 days before the pin with a push
// from O2, and the merge MX's two lines (H1 -> O2, Z1 -> O1) both go below O2 without a trusted state.
func floorRepo(oldestMinutes int) *fakeRepo {
	f := newFakeRepo()
	now := 500 * 24 * 60
	f.pinnedAt = at(now)
	f.add("R", 0)
	f.add("O1", 1, "R")
	f.add("O2", 2, "O1")
	f.add("Q", 3, "O2")
	f.add("H1", now-100, "O2")
	f.add("Z1", 2, "O1")
	f.add("MX", now-50, "H1", "Z1")
	f.push("push", "O2", "Q", oldestMinutes)
	f.head = oid("MX")
	return f
}

func TestNoMergedPRsSkipsEventsAndStopsProbesAtTheActivityFloor(t *testing.T) {
	f := floorRepo(10)
	now := 500 * 24 * 60
	res := runFake(t, f, at(now-60), at(now))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if f.calls["events"] != 0 || f.calls["search"] != 0 {
		t.Fatalf("events=%d searches=%d, want no PR queries in a repository without merged PRs", f.calls["events"], f.calls["search"])
	}
	if res.run.floor != oid("O2") {
		t.Fatalf("floor=%s want O2", short(res.run.floor))
	}
	for _, name := range []string{"O1", "R"} {
		if strictlyTrusted(res, name) {
			t.Fatalf("%s is below the floor; the probe must stop before evaluating it", name)
		}
	}
	if r := res.row(t, "Z1"); r.step != oid("MX") {
		t.Fatalf("Z1 via %s, want MX (parent 1)", short(r.step))
	}
}

func TestActivityFloorNeedsAnOldLogAndNoMergedPRs(t *testing.T) {
	now := 500 * 24 * 60
	recent := floorRepo(now - 400*24*60)
	res := runFake(t, recent, at(now-60), at(now))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.run.floor != "" || !strictlyTrusted(res, "R") {
		t.Fatalf("floor=%q: an activity log younger than 410 days gives no floor, probes walk to the root", short(res.run.floor))
	}

	withPRs := floorRepo(10)
	two := 2
	withPRs.prCount = &two
	res2 := runFake(t, withPRs, at(now-60), at(now))
	if res2.err != nil {
		t.Fatal(res2.err)
	}
	if res2.run.floor != "" || !strictlyTrusted(res2, "R") {
		t.Fatal("a repository with merged PRs gets no floor")
	}
	if withPRs.calls["events"] == 0 {
		t.Fatal("with merged PRs the MergedEvent lookup keeps its trigger")
	}
}

func TestProbeOctopusMergeFollowsTheParentWithTheNewestTrustedState(t *testing.T) {
	f := noActivity(baseRepo())
	f.add("A", 60, "M0")
	f.add("B", 61, "M0")
	f.add("C", 62, "M0")
	f.add("O", 150, "A", "B", "C")
	f.suites[oid("A")] = []checkSuite{{CreatedAt: at(120), Branch: "main", HasPush: true, PreviousSha: oid("M0")}}
	f.suites[oid("B")] = []checkSuite{{CreatedAt: at(130), Branch: "main", HasPush: true, PreviousSha: oid("M0")}}
	f.suites[oid("C")] = []checkSuite{{CreatedAt: at(145), Branch: "main", HasPush: true, PreviousSha: oid("M0")}}
	f.suites[oid("O")] = []checkSuite{{CreatedAt: at(150), Branch: "main", HasPush: true, PreviousSha: oid("C")}}
	f.head = oid("O")
	res := runFake(t, f, at(iStart), at(iEnd))
	if res.err != nil {
		t.Fatal(res.err)
	}
	var chain []string
	for _, st := range res.run.steps {
		chain = append(chain, short(st.oid))
	}
	if got, want := strings.Join(chain, " "), short(oid("M0"))+" "+short(oid("C"))+" "+short(oid("O")); got != want {
		t.Fatalf("chain %s, want M0 C O (parent 3 has the newest trusted state)", got)
	}
	expectExact(t, res, "C", 145, srcCI)
	for _, name := range []string{"A", "B", "O"} {
		expectExact(t, res, name, 150, srcCI)
	}
}
