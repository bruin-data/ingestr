package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

// Landing times of the commits table: when a commit reached the default branch B, not when it was
// written. The design, every rule's evidence and the decisions behind them are in the landed-commits
// plan (PLAN.md, FULL_PROTO.md, Q10_PROTO.md, PLAN_REVIEW.md).

const (
	relandTolerance    = 10 * time.Second
	invariantTolerance = 2 * time.Second
	renameTolerance    = 10 * time.Second
	ciRetention        = 410 * 24 * time.Hour
	defaultInterval    = 24 * time.Hour
	zeroSHA            = "0000000000000000000000000000000000000000"

	srcPR        = "pr_merged_at"
	srcActivity  = "activity_log"
	srcCI        = "ci_record"
	srcSigned    = "github_signed"
	srcClient    = "client_date"
	windowSuffix = "+window"
)

type trustKind int

const (
	trustNone trustKind = iota
	// trustFull: a server-side landing time (button PR merge, activity push, CI push witness).
	trustFull
	// trustHalf: GitHub-signed without a PR into B; a lower bound only.
	trustHalf
	// trustUpper: a PR GitHub detected from a push (Q9) or rule (c); mergedAt is an upper bound only.
	trustUpper
)

type trustInfo struct {
	source string
	at     time.Time
	kind   trustKind
	pr     int
	lower  *time.Time
}

type chainStep struct {
	oid    string
	commit *ghCommit
	trust  trustInfo
	cover  int
	ct     *time.Time
	min    *time.Time
	max    time.Time
	exact  bool
	est    time.Time
}

type landedRow struct {
	sha         string
	step        string
	landed      time.Time
	min         *time.Time
	max         time.Time
	exact       bool
	source      string
	pr          int
	removedAt   *time.Time
	parents     []string
	committedAt *time.Time
	authoredAt  *time.Time
	signed      bool
	raw         map[string]interface{}
	included    bool
	widened     []string
}

type landedRun struct {
	api    landedAPI
	branch string
	start  time.Time
	end    time.Time
	now    time.Time
	head   string
	warnf  func(format string, args ...interface{})

	activityOK  bool
	activity    []activityEntry
	actByAfter  map[string][]activityEntry
	oldNames    map[string]bool
	aliases     map[string]bool
	actLookups  map[string]*activityEntry
	ciDisabled  bool
	ciLookups   map[string]*checkSuite
	trusts      [2]map[string]trustInfo
	conflicts   map[int]bool
	onBranch    map[string]bool
	statuses    map[string]string
	events      map[string][]eventPR
	bulkPRs     map[string][]ghPR
	bulkSince   *time.Time
	bulkList    []ghPR
	relanded    map[string]bool
	ruleCs      map[string]trustInfo
	steps       []*chainStep
	setFailures []string
	// landedBy is an upper bound of the landing time of the walk's current position and of every
	// commit below it: the earliest full or upper-bound time of the newer chain steps, else now.
	landedBy time.Time

	mergedPRs    int
	floor        string
	floorAt      time.Time
	belowFloor   map[string]bool
	firstTrusted map[string]probeHit
	probeSkip    map[string]string
	aheadSets    map[string]aheadEntry
}

func newLandedRun(api landedAPI, branch string, start, end time.Time, warnf func(string, ...interface{})) *landedRun {
	return &landedRun{
		api:        api,
		branch:     branch,
		start:      start.UTC(),
		end:        end.UTC(),
		warnf:      warnf,
		actByAfter: map[string][]activityEntry{},
		oldNames:   map[string]bool{},
		aliases:    map[string]bool{},
		actLookups: map[string]*activityEntry{},
		ciLookups:  map[string]*checkSuite{},
		trusts:     [2]map[string]trustInfo{{}, {}},
		conflicts:  map[int]bool{},
		onBranch:   map[string]bool{},
		statuses:   map[string]string{},
		events:     map[string][]eventPR{},
		relanded:   map[string]bool{},
		ruleCs:     map[string]trustInfo{},
		mergedPRs:  -1,

		belowFloor:   map[string]bool{},
		firstTrusted: map[string]probeHit{},
		probeSkip:    map[string]string{},
		aheadSets:    map[string]aheadEntry{},
	}
}

// collect runs the whole algorithm and returns the rows to write: the commits whose landing window
// overlaps [start, end), plus commits a force push removed from B since start (Q3b).
func (r *landedRun) collect(ctx context.Context) ([]*landedRow, error) {
	head, pinnedAt, empty, err := r.api.branchHead(ctx, r.branch)
	if err != nil {
		return nil, err
	}
	if empty {
		config.Debug("[GITHUB] repository has no commits yet")
		return nil, nil
	}
	r.head, r.now, r.landedBy = head, pinnedAt, pinnedAt
	config.Debug("[GITHUB] commits: head of %s pinned at %s (%s); interval [%s, %s)", r.branch, short(head), pinnedAt.Format(time.RFC3339), r.start.Format(time.RFC3339), r.end.Format(time.RFC3339))

	r.mergedPRs, err = r.api.mergedPRCount(ctx)
	if err != nil {
		return nil, err
	}
	r.fetchActivity(ctx)
	r.prepareActivityFloor(ctx)

	chain, err := r.walk(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.buildSteps(ctx, chain); err != nil {
		return nil, err
	}
	rows, err := r.emitRows(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.applyForcePushLandings(ctx, rows); err != nil {
		return nil, err
	}
	removed, err := r.removedRows(ctx)
	if err != nil {
		return nil, err
	}
	if len(r.setFailures) > 0 {
		return nil, fmt.Errorf("commits: set invariant violated, nothing written: %s", strings.Join(r.setFailures, "; "))
	}
	r.checkPinnedHead(ctx)

	all := append(rows, removed...)
	r.checkTimeInvariants(all)
	r.checkI6(ctx, all)
	r.checkI9(ctx, all)

	var out []*landedRow
	for _, row := range all {
		if row.removedAt != nil || row.included || r.overlapsRow(row) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].landed.Equal(out[j].landed) {
			return out[i].landed.Before(out[j].landed)
		}
		return out[i].sha < out[j].sha
	})
	return out, nil
}

// ---------------------------------------------------------------- activity log (enrichment)

// fetchActivity reads B's activity back to the first entry older than start, plus the activity of
// B's former names (Q4: a rename is a branch_creation on the new name and a branch_deletion of the
// same sha on the old name within a few seconds). Entries after the pinned head's time are dropped
// so the activity and the walk see the same B. Unreachable activity is a warning, never an error.
func (r *landedRun) fetchActivity(ctx context.Context) {
	entries, err := r.api.activity(ctx, url.Values{"ref": {"refs/heads/" + r.branch}}, r.start)
	if err != nil {
		r.warnf("commits: activity log unavailable (%v); continuing without activity enrichment; force-pushed-away commits cannot be marked", err)
		return
	}
	r.activityOK = true
	if len(entries) == 0 || !entries[len(entries)-1].Timestamp.Before(r.start) {
		oldest := "none"
		if len(entries) > 0 {
			oldest = entries[len(entries)-1].Timestamp.Format(time.RFC3339)
		}
		r.warnf("commits: the activity log does not reach %s (oldest entry: %s); earlier steps use windows", r.start.Format(time.RFC3339), oldest)
	}
	var all []activityEntry
	for _, e := range entries {
		if e.Type != "branch_deletion" && !e.Timestamp.After(r.now) {
			all = append(all, e)
		}
	}

	seen := map[string]bool{}
	var deletions []activityEntry
	deletionsRead := false
	for round := 0; round < 10; round++ {
		var fresh []activityEntry
		for _, e := range all {
			key := e.After + "|" + e.Ref + "|" + e.Timestamp.String()
			if e.Type == "branch_creation" && e.Before == zeroSHA && !seen[key] {
				seen[key] = true
				fresh = append(fresh, e)
			}
		}
		if len(fresh) == 0 {
			break
		}
		if !deletionsRead {
			oldest := fresh[0].Timestamp
			for _, e := range fresh {
				if e.Timestamp.Before(oldest) {
					oldest = e.Timestamp
				}
			}
			deletions, err = r.api.activity(ctx, url.Values{"activity_type": {"branch_deletion"}}, oldest)
			if err != nil {
				r.warnf("commits: branch_deletion activity unavailable (%v); former names of %s not read", err, r.branch)
				break
			}
			deletionsRead = true
		}
		for _, cre := range fresh {
			for _, del := range deletions {
				if del.Type != "branch_deletion" || del.Before != cre.After || del.Ref == cre.Ref || absDuration(del.Timestamp.Sub(cre.Timestamp)) > renameTolerance {
					continue
				}
				old := del.Ref
				if r.oldNames[old] || old == "refs/heads/"+r.branch {
					continue
				}
				r.oldNames[old] = true
				config.Debug("[GITHUB] rename: %s created at %s with %s, %s deleted at %s with the same sha; reading its activity", cre.Ref, cre.Timestamp.Format(time.RFC3339), short(cre.After), old, del.Timestamp.Format(time.RFC3339))
				olds, err := r.api.activity(ctx, url.Values{"ref": {old}}, r.start)
				if err != nil {
					r.warnf("commits: activity of former branch name %s unavailable (%v)", old, err)
					continue
				}
				for _, e := range olds {
					if e.Type != "branch_deletion" && !e.Timestamp.After(del.Timestamp) && !e.Timestamp.After(r.now) {
						all = append(all, e)
					}
				}
			}
		}
	}

	dedup := map[activityEntry]bool{}
	for _, e := range all {
		if e.Type == "branch_creation" || e.Type == "branch_deletion" || dedup[e] {
			continue
		}
		dedup[e] = true
		r.activity = append(r.activity, e)
		r.actByAfter[e.After] = append(r.actByAfter[e.After], e)
	}
	sort.SliceStable(r.activity, func(i, j int) bool { return r.activity[i].Timestamp.Before(r.activity[j].Timestamp) })
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (r *landedRun) status(ctx context.Context, base, head string) (string, error) {
	key := base + "..." + head
	if st, ok := r.statuses[key]; ok {
		return st, nil
	}
	st, err := r.api.compareStatus(ctx, base, head)
	if err != nil {
		return "", err
	}
	r.statuses[key] = st
	return st, nil
}

// actLookup is the latest activity entry that brought oid onto B (Q2: landed_at is the last time a
// commit entered B). A force_push counts only when oid is on the ahead side of before...after, so a
// reset to an old commit is not a landing.
func (r *landedRun) actLookup(ctx context.Context, oid string) (*activityEntry, error) {
	if e, ok := r.actLookups[oid]; ok {
		return e, nil
	}
	cands := append([]activityEntry(nil), r.actByAfter[oid]...)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Timestamp.After(cands[j].Timestamp) })
	var latest *activityEntry
	n := 0
	for i := range cands {
		e := cands[i]
		if e.Before == "" {
			continue
		}
		if e.Type == "force_push" {
			st, err := r.status(ctx, e.Before, oid)
			if err != nil {
				return nil, err
			}
			if st != "ahead" && st != "diverged" {
				continue
			}
		}
		n++
		if latest == nil {
			latest = &e
		}
	}
	if n > 1 {
		r.relanded[oid] = true
		config.Debug("[GITHUB] Q2: %s landed %d times; using the latest landing %s", short(oid), n, latest.Timestamp.Format(time.RFC3339))
	}
	r.actLookups[oid] = latest
	return latest, nil
}

// ---------------------------------------------------------------- CI record (enrichment)

// ciLookup is the earliest check suite created by a push to B on this commit (Q7: on by default).
// No permission (e.g. a fine-grained token) is a warning and turns CI off for the run. GitHub deletes
// check data after at most 400 days, so a commit is not asked when the upper bound of its landing
// (landedBy, not its own date: an old commit can be pushed today) is older than ciRetention.
func (r *landedRun) ciLookup(ctx context.Context, c *ghCommit) (*checkSuite, error) {
	if cs, ok := r.ciLookups[c.Oid]; ok {
		return cs, nil
	}
	if r.ciDisabled || r.landedBy.Before(r.now.Add(-ciRetention)) {
		return nil, nil
	}
	suites, err := r.api.checkSuites(ctx, c.Oid)
	if errors.Is(err, errNoPermission) {
		r.warnf("commits: check suites not readable (%v); ci_record disabled for this run, windows stay", err)
		r.ciDisabled = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var best *checkSuite
	for i := range suites {
		s := suites[i]
		if !s.HasPush || s.Branch != r.branch {
			continue
		}
		if best == nil || s.CreatedAt.Before(best.CreatedAt) {
			best = &s
		}
	}
	r.ciLookups[c.Oid] = best
	return best, nil
}

// ---------------------------------------------------------------- pull requests

// isB: the PR's base is B or one of B's former names (old names from the activity log, or a name
// that GitHub's branch lookup redirects to B).
func (r *landedRun) isB(ctx context.Context, base string) (bool, error) {
	if base == r.branch || r.oldNames["refs/heads/"+base] {
		return true, nil
	}
	if v, ok := r.aliases[base]; ok {
		return v, nil
	}
	name, err := r.api.resolveBranch(ctx, base)
	if err != nil {
		return false, err
	}
	r.aliases[base] = name == r.branch
	if name == r.branch {
		config.Debug("[GITHUB] branch name %q resolves to %s (renamed); PRs into it count as PRs into %s", base, r.branch, r.branch)
	}
	return name == r.branch, nil
}

// prsOf is the commit's associated PRs. When the list is longer than the 10 the history page
// carries, the bulk merged-PR list fills in the PRs whose mergeCommit is this commit (Kerem, 9 Nov).
func (r *landedRun) prsOf(ctx context.Context, c *ghCommit) ([]ghPR, error) {
	if c.PRsTotal <= len(c.PRs) {
		return c.PRs, nil
	}
	since := r.start
	if floor := c.CommittedDate.Add(-24 * time.Hour); floor.Before(since) {
		since = floor
	}
	if _, err := r.mergedPRsSince(ctx, since); err != nil {
		return nil, err
	}
	out := append([]ghPR(nil), c.PRs...)
	have := map[int]bool{}
	for _, pr := range out {
		have[pr.Number] = true
	}
	for _, pr := range r.bulkPRs[c.Oid] {
		if !have[pr.Number] {
			out = append(out, pr)
		}
	}
	config.Debug("[GITHUB] %s is in %d PRs (page lists %d); bulk list added %d", short(c.Oid), c.PRsTotal, len(c.PRs), len(out)-len(c.PRs))
	return out, nil
}

// mergedPRsSince is the bulk list of merged PRs updated since the given time, fetched once per run
// and again only when an older start is needed.
func (r *landedRun) mergedPRsSince(ctx context.Context, since time.Time) ([]ghPR, error) {
	if r.bulkSince != nil && !since.Before(*r.bulkSince) {
		return r.bulkList, nil
	}
	prs, err := r.api.mergedPullRequests(ctx, since)
	if err != nil {
		return nil, err
	}
	r.bulkList = prs
	r.bulkPRs = map[string][]ghPR{}
	for _, pr := range prs {
		if pr.MergeCommit != "" {
			r.bulkPRs[pr.MergeCommit] = append(r.bulkPRs[pr.MergeCommit], pr)
		}
	}
	r.bulkSince = &since
	return prs, nil
}

func prHeadIsStep(c *ghCommit) bool {
	for _, pr := range c.PRs {
		if pr.MergedAt != nil && pr.MergeCommit == c.Oid && pr.HeadOid == c.Oid {
			return true
		}
	}
	return false
}

// mergedEventLookupWanted is when the MergedEvent lookup (Q10 (e)) is sent: never in a repository
// without merged PRs (D2), otherwise for a commit that no merged PR names as its mergeCommit and that
// is a merge or lists a merged PR (daily cost +2–4 points, Q10_PROTO §8).
func (r *landedRun) mergedEventLookupWanted(c *ghCommit) bool {
	if r.mergedPRs == 0 {
		return false
	}
	hasMerged := false
	for _, pr := range c.PRs {
		if pr.MergedAt == nil {
			continue
		}
		if pr.MergeCommit == c.Oid {
			return false
		}
		hasMerged = true
	}
	return len(c.Parents) >= 2 || hasMerged
}

func eventRequestFor(c *ghCommit) eventRequest {
	if len(c.Parents) >= 2 {
		return eventRequest{Commit: c.Oid, Parent2: c.Parents[1], Own: len(c.PRs) > 0}
	}
	return eventRequest{Commit: c.Oid, Own: true}
}

// eventPRs is the PRs (with all their MergedEvent commits) around c, fetched in one aliased query
// together with the other commits of c's history page that need the lookup.
func (r *landedRun) eventPRs(ctx context.Context, c *ghCommit) ([]eventPR, error) {
	if evs, ok := r.events[c.Oid]; ok || r.mergedPRs == 0 {
		return evs, nil
	}
	reqs := []eventRequest{eventRequestFor(c)}
	for _, m := range r.api.pageMates(c.Oid) {
		if m.Oid == c.Oid || !r.mergedEventLookupWanted(m) {
			continue
		}
		if _, ok := r.events[m.Oid]; ok {
			continue
		}
		reqs = append(reqs, eventRequestFor(m))
		if len(reqs) >= 2*eventBatchSize {
			break
		}
	}
	got, err := r.api.mergedEvents(ctx, reqs)
	if err != nil {
		return nil, err
	}
	for _, q := range reqs {
		r.events[q.Commit] = got[q.Commit]
	}
	return r.events[c.Oid], nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (r *landedRun) onB(ctx context.Context, oid string) (bool, error) {
	if v, ok := r.onBranch[oid]; ok {
		return v, nil
	}
	st, err := r.status(ctx, oid, r.head)
	if err != nil {
		return false, err
	}
	r.onBranch[oid] = st == "ahead" || st == "identical"
	return r.onBranch[oid], nil
}

// ---------------------------------------------------------------- trust (PLAN §3)

func (r *landedRun) trust(ctx context.Context, oid string, strict bool) (trustInfo, error) {
	idx := 0
	if strict {
		idx = 1
	}
	if t, ok := r.trusts[idx][oid]; ok {
		return t, nil
	}
	t, err := r.trustCompute(ctx, oid, strict)
	if err != nil {
		return trustInfo{}, err
	}
	r.trusts[idx][oid] = t
	return t, nil
}

// trustCompute applies PLAN §3 top to bottom. strict (probes, Q8) accepts only states proven to be
// B heads: button-merge PRs, activity, CI; no upper bounds (Q9) and no signed-only commits.
func (r *landedRun) trustCompute(ctx context.Context, oid string, strict bool) (trustInfo, error) {
	c, err := r.api.commit(ctx, oid)
	if err != nil {
		return trustInfo{}, err
	}
	var full, upper *trustInfo
	consider := func(pr ghPR) (bool, error) {
		if pr.MergedAt == nil || r.conflicts[pr.Number] {
			return false, nil
		}
		ok, err := r.isB(ctx, pr.BaseRef)
		if err != nil || !ok {
			return false, err
		}
		if pr.HeadOid != oid {
			full = &trustInfo{source: srcPR, at: *pr.MergedAt, kind: trustFull, pr: pr.Number}
			return true, nil
		}
		if upper == nil || pr.MergedAt.Before(upper.at) {
			upper = &trustInfo{source: srcPR, at: *pr.MergedAt, kind: trustUpper, pr: pr.Number}
		}
		return false, nil
	}

	prs, err := r.prsOf(ctx, c)
	if err != nil {
		return trustInfo{}, err
	}
	for _, pr := range prs {
		if pr.MergeCommit != oid {
			continue
		}
		done, err := consider(pr)
		if err != nil {
			return trustInfo{}, err
		}
		if done {
			break
		}
	}

	if full == nil && upper == nil && r.mergedEventLookupWanted(c) {
		evs, err := r.eventPRs(ctx, c)
		if err != nil {
			return trustInfo{}, err
		}
		for _, ev := range evs {
			if !containsString(ev.EventCommits, oid) || r.conflicts[ev.Number] {
				continue
			}
			if ok, err := r.isB(ctx, ev.BaseRef); err != nil {
				return trustInfo{}, err
			} else if !ok {
				continue
			}
			if ev.MergeCommit != "" && ev.MergeCommit != oid {
				onB, err := r.onB(ctx, ev.MergeCommit)
				if err != nil {
					return trustInfo{}, err
				}
				if onB {
					r.warnf("commits: conflict in PR #%d: mergeCommit %s and MergedEvent commit %s are both on %s; trusting neither", ev.Number, short(ev.MergeCommit), short(oid), r.branch)
					r.conflicts[ev.Number] = true
					continue
				}
			}
			done, err := consider(ev.ghPR)
			if err != nil {
				return trustInfo{}, err
			}
			if done {
				break
			}
		}
	}

	if full != nil {
		if r.activityOK {
			a, err := r.actLookup(ctx, oid)
			if err != nil {
				return trustInfo{}, err
			}
			if a != nil && a.Timestamp.Sub(full.at) > relandTolerance {
				r.relanded[oid] = true
				return trustInfo{source: srcActivity, at: a.Timestamp, kind: trustFull}, nil
			}
		}
		return *full, nil
	}
	if r.activityOK {
		a, err := r.actLookup(ctx, oid)
		if err != nil {
			return trustInfo{}, err
		}
		if a != nil {
			return trustInfo{source: srcActivity, at: a.Timestamp, kind: trustFull}, nil
		}
	}
	cs, err := r.ciLookup(ctx, c)
	if err != nil {
		return trustInfo{}, err
	}
	if cs != nil {
		return trustInfo{source: srcCI, at: cs.CreatedAt, kind: trustFull}, nil
	}
	if strict {
		return trustInfo{}, nil
	}
	if upper != nil {
		if c.SignedByGitHub {
			cd := c.CommittedDate
			upper.lower = &cd
		}
		return *upper, nil
	}
	if c.SignedByGitHub {
		cd := c.CommittedDate
		return trustInfo{source: srcSigned, at: cd, kind: trustHalf, lower: &cd}, nil
	}
	return trustInfo{}, nil
}

// ruleC (Kerem, 8 Nov): a merge step followed through parent 1 whose parent 2 is the head and the
// MergedEvent commit of a PR into B (a local merge GitHub detected from the push) has that PR's
// mergedAt as an upper bound.
func (r *landedRun) ruleC(ctx context.Context, c *ghCommit) (trustInfo, bool, error) {
	if len(c.Parents) < 2 {
		return trustInfo{}, false, nil
	}
	p2 := c.Parents[1]
	evs, err := r.eventPRs(ctx, c)
	if err != nil {
		return trustInfo{}, false, err
	}
	var best *trustInfo
	for _, ev := range evs {
		if !ev.FromParent2 || ev.HeadOid != p2 || !containsString(ev.EventCommits, p2) || r.conflicts[ev.Number] {
			continue
		}
		ok, err := r.isB(ctx, ev.BaseRef)
		if err != nil {
			return trustInfo{}, false, err
		}
		if ok && (best == nil || ev.MergedAt.Before(best.at)) {
			best = &trustInfo{source: srcPR, at: *ev.MergedAt, kind: trustUpper, pr: ev.Number}
		}
	}
	if best == nil {
		return trustInfo{}, false, nil
	}
	return *best, true, nil
}

// ---------------------------------------------------------------- walk (PLAN §2, §4)

// walk follows B's chain of states from the pinned head back to the first state with a trusted
// (full or upper-bound) time before start, or to the root. Returns newest first.
func (r *landedRun) walk(ctx context.Context) ([]string, error) {
	var chain []string
	visited := map[string]bool{}
	cur := r.head
	for cur != "" {
		if visited[cur] {
			return nil, fmt.Errorf("commits: chain walk revisited %s", cur)
		}
		visited[cur] = true
		chain = append(chain, cur)
		t, err := r.trust(ctx, cur, false)
		if err != nil {
			return nil, err
		}
		if t.kind == trustFull || t.kind == trustUpper {
			r.boundLanding(t.at)
		}
		if (t.kind == trustFull || t.kind == trustUpper) && t.at.Before(r.start) {
			break
		}
		c, err := r.api.commit(ctx, cur)
		if err != nil {
			return nil, err
		}
		if len(c.Parents) == 0 {
			break
		}
		next := 0
		if len(c.Parents) > 1 {
			if !isGitHubPRMerge(t, c) {
				next, err = r.probeParents(ctx, c)
				if err != nil {
					return nil, err
				}
				config.Debug("[GITHUB] merge %s: follow parent %d %s", short(cur), next+1, short(c.Parents[next]))
			}
			if next == 0 && t.kind != trustFull {
				rc, ok, err := r.ruleC(ctx, c)
				if err != nil {
					return nil, err
				}
				if ok {
					r.ruleCs[cur] = rc
					r.boundLanding(rc.at)
					if rc.at.Before(r.start) {
						break
					}
				}
			}
		}
		cur = c.Parents[next]
	}
	return chain, nil
}

func (r *landedRun) boundLanding(at time.Time) {
	if at.Before(r.landedBy) {
		r.landedBy = at
	}
}

// isGitHubPRMerge: GitHub made this merge for a PR into B (signed, PR trust, and not a PR whose head
// is the merge itself), so parent 1 is B's previous state (w1-p1, Q1).
func isGitHubPRMerge(t trustInfo, c *ghCommit) bool {
	return t.source == srcPR && t.kind == trustFull && c.SignedByGitHub && !prHeadIsStep(c)
}

// ---------------------------------------------------------------- windows (PLAN §6)

func (r *landedRun) buildSteps(ctx context.Context, chain []string) error {
	r.steps = nil
	for i := len(chain) - 1; i >= 0; i-- {
		oid := chain[i]
		t, err := r.trust(ctx, oid, false)
		if err != nil {
			return err
		}
		if rc, ok := r.ruleCs[oid]; ok && t.kind != trustFull {
			switch t.kind {
			case trustUpper:
				if rc.at.Before(t.at) {
					t.at, t.pr = rc.at, rc.pr
				}
			case trustHalf:
				lower := t.at
				t = trustInfo{source: srcPR, at: rc.at, kind: trustUpper, pr: rc.pr, lower: &lower}
			default:
				t = rc
			}
		}
		c, err := r.api.commit(ctx, oid)
		if err != nil {
			return err
		}
		r.steps = append(r.steps, &chainStep{oid: oid, commit: c, trust: t, cover: -1})
	}
	if err := r.coverSteps(ctx); err != nil {
		return err
	}
	r.assignWindows()
	return nil
}

// coverSteps: an activity push or CI suite whose previous head is an earlier chain step lands every
// step in between at its time; for a PR step the push range makes its commits exact at the push time
// (Q5). PR steps with a full time keep it; upper-bound PR steps (Q9, rule c) are replaced.
func (r *landedRun) coverSteps(ctx context.Context) error {
	if !r.activityOK && r.ciDisabled {
		return nil
	}
	for i, st := range r.steps {
		if st.cover >= 0 {
			continue
		}
		var before string
		var at *time.Time
		switch {
		case st.trust.source == srcActivity:
			a, err := r.actLookup(ctx, st.oid)
			if err != nil {
				return err
			}
			if a != nil {
				before = a.Before
			}
		case st.trust.source == srcCI:
			cs, err := r.ciLookup(ctx, st.commit)
			if err != nil {
				return err
			}
			if cs != nil {
				before = cs.PreviousSha
			}
		case st.trust.source == srcPR && st.trust.kind == trustFull && r.activityOK:
			a, err := r.actLookup(ctx, st.oid)
			if err != nil {
				return err
			}
			if a != nil {
				before, at = a.Before, &a.Timestamp
			}
		default:
			continue
		}
		if before == "" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if r.steps[j].oid != before {
				continue
			}
			st.cover = j
			src, ct := st.trust.source, st.trust.at
			if st.trust.source == srcPR {
				st.ct = at
				src, ct = srcActivity, *at
				config.Debug("[GITHUB] Q5: PR step %s: push %s->%s at %s covers %d earlier step(s)", short(st.oid), short(before), short(st.oid), at.Format(time.RFC3339), i-j-1)
			}
			for k := j + 1; k < i; k++ {
				if r.steps[k].trust.source != srcPR || r.steps[k].trust.kind == trustUpper {
					r.steps[k].trust = trustInfo{source: src, at: ct, kind: trustFull}
					r.steps[k].cover = j
				}
			}
			break
		}
	}
	return nil
}

func (r *landedRun) assignWindows() {
	var last *time.Time
	for _, st := range r.steps {
		st.min = last
		if st.trust.lower != nil && (st.min == nil || st.trust.lower.After(*st.min)) {
			lower := *st.trust.lower
			st.min = &lower
		}
		switch {
		case st.trust.kind == trustFull || st.trust.kind == trustHalf:
			if last == nil || st.trust.at.After(*last) {
				at := st.trust.at
				last = &at
			}
		case st.trust.lower != nil:
			if last == nil || st.trust.lower.After(*last) {
				lower := *st.trust.lower
				last = &lower
			}
		}
	}
	next := r.now
	for i := len(r.steps) - 1; i >= 0; i-- {
		st := r.steps[i]
		if st.trust.kind == trustFull {
			next = st.trust.at
		} else if st.trust.kind == trustUpper && st.trust.at.Before(next) {
			next = st.trust.at
		}
		st.max = next
	}
	for i, st := range r.steps {
		if st.trust.kind != trustFull {
			continue
		}
		switch st.trust.source {
		case srcActivity, srcCI:
			st.exact = st.cover >= 0
		default:
			st.exact = i > 0 && !prHeadIsStep(st.commit) && st.commit.SignedByGitHub &&
				len(st.commit.Parents) > 0 && st.commit.Parents[0] == r.steps[i-1].oid
		}
		if st.exact {
			at := st.trust.at
			st.min = &at
		}
	}
	var prev *time.Time
	for _, st := range r.steps {
		e := st.trust.at
		if st.trust.kind != trustFull {
			e = st.commit.CommittedDate
			if st.min != nil && e.Before(*st.min) {
				e = *st.min
			}
			if e.After(st.max) {
				e = st.max
			}
			if prev != nil && e.Before(*prev) {
				e = *prev
			}
		}
		if st.min != nil && st.min.After(st.max) {
			r.warnf("commits: step %s has min %s > max %s (%s)", short(st.oid), st.min.Format(time.RFC3339), st.max.Format(time.RFC3339), st.trust.source)
		}
		st.est = e
		ee := e
		prev = &ee
	}
}

func (r *landedRun) inInterval(t time.Time) bool {
	return !t.Before(r.start) && t.Before(r.end)
}

func (r *landedRun) overlaps(exact bool, at time.Time, lo *time.Time, hi time.Time) bool {
	if exact {
		return r.inInterval(at)
	}
	return !hi.Before(r.start) && (lo == nil || lo.Before(r.end))
}

func (r *landedRun) overlapsRow(row *landedRow) bool {
	return r.overlaps(row.exact, row.landed, row.min, row.max)
}

func (r *landedRun) stepOverlaps(st *chainStep) bool {
	return r.overlaps(st.exact, st.trust.at, st.min, st.max)
}

// ---------------------------------------------------------------- rows

func (r *landedRun) emitRows(ctx context.Context) ([]*landedRow, error) {
	var rows []*landedRow
	seen := map[string]bool{}
	add := func(i int, raw map[string]interface{}) {
		row := r.rowFor(i, raw)
		if seen[row.sha] {
			return
		}
		seen[row.sha] = true
		row.included = r.overlapsRow(row)
		rows = append(rows, row)
	}
	if len(r.steps) == 0 {
		return nil, nil
	}
	s0 := r.steps[0]
	upperBeforeStart := s0.trust.kind == trustUpper && s0.trust.at.Before(r.start)
	if s0.trust.kind != trustFull && !upperBeforeStart {
		raw, err := r.api.restCommit(ctx, s0.oid)
		if err != nil {
			return nil, err
		}
		add(0, raw)
	}
	for i := 1; i < len(r.steps); i++ {
		if !r.stepOverlaps(r.steps[i]) {
			continue
		}
		commits, err := r.stepCommits(ctx, r.steps[i-1].oid, r.steps[i].oid)
		if err != nil {
			return nil, err
		}
		for _, raw := range commits {
			add(i, raw)
		}
	}
	return rows, nil
}

func (r *landedRun) rowFor(i int, raw map[string]interface{}) *landedRow {
	st := r.steps[i]
	row := newRowFromREST(raw)
	row.step = st.oid
	row.source = st.trust.source
	if row.source == "" {
		row.source = srcClient
	}
	if st.trust.source == srcPR {
		row.pr = st.trust.pr
	}
	row.exact, row.min, row.max, row.landed = st.exact, st.min, st.max, st.est
	switch st.trust.kind {
	case trustFull:
		switch {
		case row.sha == st.oid:
			row.exact = true
			est := st.est
			row.min = &est
		case !row.exact && st.ct != nil:
			ct := *st.ct
			row.exact, row.source, row.landed, row.min, row.max, row.pr = true, srcActivity, ct, &ct, ct, 0
		case !row.exact:
			row.source += windowSuffix
		}
	case trustHalf, trustUpper:
		row.source += windowSuffix
	}
	return row
}

func newRowFromREST(raw map[string]interface{}) *landedRow {
	row := &landedRow{raw: raw}
	row.sha, _ = raw["sha"].(string)
	if ps, ok := raw["parents"].([]interface{}); ok {
		for _, p := range ps {
			if pm, ok := p.(map[string]interface{}); ok {
				if sha, ok := pm["sha"].(string); ok {
					row.parents = append(row.parents, sha)
				}
			}
		}
	}
	commit, _ := raw["commit"].(map[string]interface{})
	dateOf := func(key string) *time.Time {
		who, _ := commit[key].(map[string]interface{})
		s, _ := who["date"].(string)
		if t, err := parseGitHubTime(s); err == nil {
			return &t
		}
		return nil
	}
	row.committedAt = dateOf("committer")
	row.authoredAt = dateOf("author")
	verification, _ := commit["verification"].(map[string]interface{})
	committer, _ := commit["committer"].(map[string]interface{})
	verified, _ := verification["verified"].(bool)
	row.signed = verified && committer["email"] == "noreply@github.com"
	return row
}

// stepCommits is every commit reachable from head and not from base (compare base...head). compare
// silently keeps only the newest 10,000 commits; the missing parts are fetched from their frontier
// heads and the union must have exactly ahead_by commits (Q6), otherwise the run fails. Unrelated
// histories (404 "No common ancestor") fall back to listing commits?sha=head.
func (r *landedRun) stepCommits(ctx context.Context, base, head string) ([]map[string]interface{}, error) {
	queue := []string{head}
	visited := map[string]bool{}
	got := map[string]map[string]interface{}{}
	var order []string
	ahead0 := -1
	truncated := 0
	for qi := 0; qi < len(queue); qi++ {
		h := queue[qi]
		if visited[h] {
			continue
		}
		visited[h] = true
		var commits []map[string]interface{}
		ahead := 0
		fallback := false
		for page := 1; page <= compareMaxPages; page++ {
			res, err := r.api.compare(ctx, base, h, page)
			if err != nil {
				return nil, err
			}
			if res.NoCommonAncestor {
				fallback = true
				break
			}
			if page == 1 {
				ahead = res.AheadBy
			}
			commits = append(commits, res.Commits...)
			if len(res.Commits) < comparePageSize {
				break
			}
		}
		if fallback {
			commits = nil
			for page := 1; ; page++ {
				cs, err := r.api.listCommits(ctx, h, page)
				if err != nil {
					return nil, err
				}
				commits = append(commits, cs...)
				if len(cs) < comparePageSize {
					break
				}
			}
			ahead = len(commits)
			config.Debug("[GITHUB] Q6: compare %s...%s: no common ancestor, listed %d commits instead", short(base), short(h), ahead)
		}
		if ahead0 < 0 {
			ahead0 = ahead
		}
		if ahead == 0 {
			continue
		}
		inPage := map[string]bool{}
		for _, c := range commits {
			sha, _ := c["sha"].(string)
			inPage[sha] = true
			if _, ok := got[sha]; !ok {
				got[sha] = c
				order = append(order, sha)
			}
		}
		if len(commits) < ahead {
			truncated++
			for _, c := range commits {
				for _, p := range newRowFromREST(c).parents {
					if !inPage[p] && got[p] == nil && !visited[p] {
						queue = append(queue, p)
					}
				}
			}
			config.Debug("[GITHUB] Q6: compare %s...%s: ahead_by=%d returned=%d (truncated)", short(base), short(h), ahead, len(commits))
		}
	}
	if len(got) != ahead0 {
		r.setFailures = append(r.setFailures, fmt.Sprintf("Q6: compare %s...%s gave %d distinct commits after splitting, ahead_by %d", short(base), short(head), len(got), ahead0))
	}
	if truncated > 0 {
		config.Debug("[GITHUB] Q6: step %s: %d commits from %d truncated compares", short(head), len(got), truncated)
	}
	out := make([]map[string]interface{}, 0, len(order))
	for _, sha := range order {
		out = append(out, got[sha])
	}
	return out, nil
}

// ---------------------------------------------------------------- force pushes (Q2, Q3)

func (r *landedRun) forcePushes(since *time.Time) []activityEntry {
	var out []activityEntry
	for _, e := range r.activity {
		if e.Type == "force_push" && (since == nil || !e.Timestamp.Before(*since)) {
			out = append(out, e)
		}
	}
	return out
}

// applyForcePushLandings (Q2): a force push lands every commit on the ahead side of before...after
// at its time, the latest landing wins; a reset to an old commit lands nothing.
func (r *landedRun) applyForcePushLandings(ctx context.Context, rows []*landedRow) error {
	if !r.activityOK {
		return nil
	}
	type landing struct {
		at   time.Time
		head string
	}
	reland := map[string]landing{}
	for _, e := range r.forcePushes(nil) {
		st, err := r.status(ctx, e.Before, e.After)
		if err != nil {
			return err
		}
		if st != "ahead" && st != "diverged" {
			if st == "not_found" {
				r.warnf("commits: Q2: force push %s->%s at %s: old head not readable; its commits keep their chain times", short(e.Before), short(e.After), e.Timestamp.Format(time.RFC3339))
			}
			continue
		}
		commits, err := r.stepCommits(ctx, e.Before, e.After)
		if err != nil {
			return err
		}
		for _, c := range commits {
			sha, _ := c["sha"].(string)
			if l, ok := reland[sha]; !ok || e.Timestamp.After(l.at) {
				reland[sha] = landing{at: e.Timestamp, head: e.After}
			}
		}
	}
	for _, row := range rows {
		l, ok := reland[row.sha]
		if !ok || (row.exact && !row.landed.Before(l.at.Add(-invariantTolerance))) {
			continue
		}
		at := l.at
		row.exact, row.landed, row.min, row.max, row.source, row.step, row.pr = true, at, &at, at, srcActivity, l.head, 0
		row.included = row.included || r.overlapsRow(row)
		r.relanded[row.sha] = true
		config.Debug("[GITHUB] Q2: %s (re-)landed by the force push to %s at %s", short(row.sha), short(l.head), at.Format(time.RFC3339))
	}
	return nil
}

// removedRows (Q3b, Kerem 8 Nov): commits a force push in [start, now) took off B and that are still
// gone get removed_from_branch_at = the force push time; their full row is emitted again so the
// merge updates it. Their landing comes from the activity entries that built the removed head.
func (r *landedRun) removedRows(ctx context.Context) ([]*landedRow, error) {
	if !r.activityOK {
		r.warnf("commits: integrity check (b) unavailable (no activity log): force-pushed-away commits cannot be marked")
		return nil, nil
	}
	latest := map[string]*landedRow{}
	for _, fp := range r.forcePushes(&r.start) {
		st, err := r.status(ctx, fp.Before, r.head)
		if err != nil {
			return nil, err
		}
		switch st {
		case "ahead", "identical":
			continue
		case "not_found":
			r.warnf("commits: Q3b: force push %s->%s at %s: old head not readable; its removed commits cannot be marked", short(fp.Before), short(fp.After), fp.Timestamp.Format(time.RFC3339))
			continue
		}
		removed, err := r.stepCommits(ctx, fp.After, fp.Before)
		if err != nil {
			return nil, err
		}
		goneList, err := r.stepCommits(ctx, r.head, fp.Before)
		if err != nil {
			return nil, err
		}
		gone := map[string]bool{}
		for _, c := range goneList {
			sha, _ := c["sha"].(string)
			gone[sha] = true
		}
		left := map[string]bool{}
		var still []map[string]interface{}
		for _, c := range removed {
			sha, _ := c["sha"].(string)
			if gone[sha] {
				still = append(still, c)
				left[sha] = true
			}
		}
		if len(still) == 0 {
			continue
		}
		lands, err := r.removedLandings(ctx, fp, left)
		if err != nil {
			return nil, err
		}
		removedAt := fp.Timestamp
		for _, c := range still {
			row := newRowFromREST(c)
			if l, ok := lands[row.sha]; ok {
				at := l.Timestamp
				row.step, row.landed, row.min, row.max, row.exact, row.source = l.After, at, &at, at, true, srcActivity
			} else {
				removedRowWithoutLanding(row, fp)
			}
			row.removedAt = &removedAt
			if prev, ok := latest[row.sha]; !ok || removedAt.After(*prev.removedAt) {
				latest[row.sha] = row
			}
		}
		config.Debug("[GITHUB] Q3b: force push %s->%s at %s removed %d commit(s) still not on %s", short(fp.Before), short(fp.After), fp.Timestamp.Format(time.RFC3339), len(still), r.branch)
	}
	out := make([]*landedRow, 0, len(latest))
	for _, row := range latest {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sha < out[j].sha })
	return out, nil
}

// removedLandings walks back the activity entries that built the removed head and returns, for each
// removed commit, the entry that landed it.
func (r *landedRun) removedLandings(ctx context.Context, fp activityEntry, left map[string]bool) (map[string]activityEntry, error) {
	lands := map[string]activityEntry{}
	x := fp.Before
	for left[x] {
		var entry *activityEntry
		for _, e := range r.actByAfter[x] {
			if e.Timestamp.Before(fp.Timestamp) && (entry == nil || e.Timestamp.After(entry.Timestamp)) {
				ec := e
				entry = &ec
			}
		}
		if entry == nil || entry.Before == zeroSHA || entry.Before == "" {
			break
		}
		commits, err := r.stepCommits(ctx, entry.Before, x)
		if err != nil {
			return nil, err
		}
		for _, c := range commits {
			sha, _ := c["sha"].(string)
			if left[sha] {
				lands[sha] = *entry
				delete(left, sha)
			}
		}
		x = entry.Before
	}
	return lands, nil
}

// removedRowWithoutLanding keeps the prototype's row for a removed commit whose landing the activity
// log does not show (w1_chain_full.sh Q3b): window (unknown, removal time], estimate = its commit
// date clamped to the removal time. Such a row can overwrite an exact landing written by an earlier
// run: a wider but correct window is accepted (Kerem, 9 Nov).
func removedRowWithoutLanding(row *landedRow, fp activityEntry) {
	est := fp.Timestamp
	if row.committedAt != nil && row.committedAt.Before(est) {
		est = *row.committedAt
	}
	row.step, row.landed, row.min, row.max, row.exact, row.source = fp.Before, est, nil, fp.Timestamp, false, srcClient+windowSuffix
}

// checkPinnedHead (Q3a): B must still contain the pinned head at the end of the run.
func (r *landedRun) checkPinnedHead(ctx context.Context) {
	live, _, _, err := r.api.branchHead(ctx, r.branch)
	if err != nil || live == "" {
		r.warnf("commits: Q3a: live head of %s not readable (%v); pinned head not re-checked", r.branch, err)
		return
	}
	if live == r.head {
		return
	}
	st, err := r.api.compareStatus(ctx, r.head, live)
	if err == nil && st == "ahead" {
		config.Debug("[GITHUB] Q3a: head advanced during the run %s -> %s; new commits belong to a later run", short(r.head), short(live))
		return
	}
	r.warnf("commits: Q3a: pinned head %s is not an ancestor of the live head %s of %s (compare status %q, %v): the branch was rewritten during the run; rows may include commits no longer on it, re-run", short(r.head), short(live), r.branch, st, err)
}
