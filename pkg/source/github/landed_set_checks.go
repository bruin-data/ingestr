package github

import (
	"context"
	"slices"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

// Set invariants I6 and I9. Both rest on GitHub side data with known errors (a PR whose mergeCommit
// points at another PR's commit, activity entries GitHub never wrote), so a violation is a warning
// and the rows are still written. Only the compare split count fails a run.

func (r *landedRun) returned(row *landedRow) bool {
	if row.removedAt != nil {
		return r.overlapsRow(row)
	}
	return row.included || r.overlapsRow(row)
}

func (r *landedRun) intervalEndSeen() time.Time {
	if r.now.Before(r.end) {
		return r.now
	}
	return r.end
}

// checkI6: every PR merged into B (or a former name) during the interval has its merge commit in the
// output, with mergedAt inside that row's window. A PR GitHub detected from a push (head = the
// commit) only gives mergedAt >= min; a commit that landed again later only mergedAt <= max.
func (r *landedRun) checkI6(ctx context.Context, rows []*landedRow) {
	if r.mergedPRs == 0 {
		config.Debug("[GITHUB] I6 skipped: the repository has no merged pull requests")
		return
	}
	prs, err := r.mergedPRsSince(ctx, r.start)
	if err != nil {
		r.warnf("commits: merged pull requests not readable (%v); skipped the check that every PR merged in the interval is returned", err)
		return
	}
	bySha := map[string]*landedRow{}
	for _, row := range rows {
		if _, ok := bySha[row.sha]; !ok {
			bySha[row.sha] = row
		}
	}
	end := r.intervalEndSeen()
	seen := map[int]bool{}
	var total, checked, upperOnly, relanded, warnings int
	for _, pr := range prs {
		if pr.MergedAt == nil || pr.MergedAt.Before(r.start) || !pr.MergedAt.Before(end) || seen[pr.Number] {
			continue
		}
		seen[pr.Number] = true
		if r.conflicts[pr.Number] {
			continue
		}
		isB, err := r.isB(ctx, pr.BaseRef)
		if err != nil {
			r.warnf("commits: base branch %q of PR #%d not resolvable (%v); PR not checked", pr.BaseRef, pr.Number, err)
			continue
		}
		if !isB {
			continue
		}
		total++
		var cands []string
		for _, c := range append([]string{pr.MergeCommit}, pr.EventCommits...) {
			if c != "" && !slices.Contains(cands, c) {
				cands = append(cands, c)
			}
		}
		var row *landedRow
		for _, c := range cands {
			if bySha[c] != nil {
				row = bySha[c]
				break
			}
		}
		at := *pr.MergedAt
		if row == nil {
			for _, c := range cands {
				on, err := r.onB(ctx, c)
				if err != nil {
					r.warnf("commits: PR #%d: could not check whether %s is on %s (%v)", pr.Number, short(c), r.branch, err)
					break
				}
				if on {
					warnings++
					r.warnf("commits: PR #%d was merged into %s at %s, its commit %s is on %s but missing from the output", pr.Number, pr.BaseRef, fmtT(&at), short(c), r.branch)
					break
				}
			}
			continue
		}
		if row.removedAt != nil {
			continue
		}
		checked++
		upper := row.sha == pr.HeadOid
		var d time.Duration
		var what string
		switch {
		case r.relanded[row.sha]:
			relanded++
			d = at.Sub(row.max)
			what = "merged after the window (the commit landed again later, so only the window end is checked)"
		case upper:
			upperOnly++
			d = -time.Hour
			if row.min != nil {
				d = row.min.Sub(at)
			}
			what = "merged before the window (GitHub detected this merge from a push, so only the window start is checked)"
		default:
			d = at.Sub(row.max)
			if row.min != nil && row.min.Sub(at) > d {
				d = row.min.Sub(at)
			}
			what = "merged outside the window"
		}
		switch {
		case d > invariantTolerance:
			warnings++
			r.warnf("commits: PR #%d was merged at %s, but its commit %s has the landing window (%s, %s] (%s via %s): %s", pr.Number, fmtT(&at), short(row.sha), fmtT(row.min), fmtT(&row.max), row.source, short(row.step), what)
		case !upper && !r.relanded[row.sha] && !r.returned(row):
			warnings++
			r.warnf("commits: PR #%d was merged at %s, but its commit %s is not returned for the interval", pr.Number, fmtT(&at), short(row.sha))
		}
	}
	config.Debug("[GITHUB] I6: %d PRs merged into %s in the interval, %d checked (%d upper-bound only, %d re-landed), %d warnings",
		total, r.branch, checked, upperOnly, relanded, warnings)
}

// checkI9: with B's heads at start and end known from the activity log and no force push between,
// compare headS...headE's ahead_by must lie between the rows whose window is inside the interval and
// the rows returned for it. Rows of commits a later force push removed count too.
func (r *landedRun) checkI9(ctx context.Context, rows []*landedRow) {
	if !r.activityOK {
		config.Debug("[GITHUB] I9 skipped: no activity log")
		return
	}
	end := r.intervalEndSeen()
	var headS, headE string
	for _, e := range r.activity {
		if e.Type == "force_push" && !e.Timestamp.Before(r.start) && e.Timestamp.Before(end) {
			config.Debug("[GITHUB] I9 skipped: force push at %s inside the interval", fmtT(&e.Timestamp))
			return
		}
		if e.Timestamp.Before(r.start) {
			headS = e.After
		}
		if e.Timestamp.Before(end) {
			headE = e.After
		}
	}
	if headS == "" {
		config.Debug("[GITHUB] I9 skipped: the activity log does not reach the interval start")
		return
	}
	ahead := 0
	if headS != headE {
		st, n, err := r.api.compareCount(ctx, headS, headE)
		if err != nil {
			r.warnf("commits: could not compare the branch heads %s...%s (%v); skipped the row count check", short(headS), short(headE), err)
			return
		}
		if st != "ahead" {
			r.warnf("commits: the activity log's branch heads %s (interval start) and %s (interval end) are not in order (compare status %q); skipped the row count check", short(headS), short(headE), st)
			return
		}
		ahead = n
	}
	inside, returned := 0, 0
	for _, row := range rows {
		if !r.returned(row) {
			continue
		}
		returned++
		if row.exact && r.inInterval(row.landed) || !row.exact && row.min != nil && !row.min.Before(r.start) && row.max.Before(r.end) {
			inside++
		}
	}
	if inside <= ahead && ahead <= returned {
		config.Debug("[GITHUB] I9: rows inside %d <= ahead_by %d (%s...%s) <= rows returned %d", inside, ahead, short(headS), short(headE), returned)
		return
	}
	r.warnf("commits: the activity log says %d commits landed in [%s, %s) (compare %s...%s), but %d rows have their landing window inside the interval and %d rows are returned",
		ahead, fmtT(&r.start), fmtT(&r.end), short(headS), short(headE), inside, returned)
}
