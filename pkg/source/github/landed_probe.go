package github

import (
	"context"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

// Parent choice at a merge that GitHub did not make for a PR: every parent's first-parent line is
// probed down to its first trusted state (strict trust: only states proven to be heads of the
// branch) and the parent reaching the newest one wins; a tie or nothing found picks parent 1. There is no budget. A probe stops early only where stopping loses nothing:
//   - meet: it reaches a commit another probe of this merge already visited; both get the same answer;
//   - ancestor stop: it reaches an ancestor of another parent's first trusted state A, so everything
//     below landed no later than A and cannot win;
//   - activity floor: in a repository without merged PRs, it reaches an ancestor of the commit
//     the branch was at before the activity log starts, more than ciRetention ago: nothing below can
//     be trusted.
// First trusted states and stop points are cached for the run (never between runs).

type probeHit struct {
	at  *time.Time
	oid string
}

type probeState struct {
	parents []string
	pos     []string
	done    []bool
	res     []probeHit
	root    []int
	path    [][]string
	bound   []int
	visited map[string]int
}

func (p *probeState) findRoot(j int) int {
	for p.root[j] != j {
		j = p.root[j]
	}
	return j
}

func (r *landedRun) probeParents(ctx context.Context, c *ghCommit) (int, error) {
	n := len(c.Parents)
	p := &probeState{
		parents: c.Parents,
		pos:     append([]string(nil), c.Parents...),
		done:    make([]bool, n),
		res:     make([]probeHit, n),
		root:    make([]int, n),
		path:    make([][]string, n),
		bound:   make([]int, n),
		visited: map[string]int{},
	}
	for j := range p.root {
		p.root[j], p.bound[j] = j, -1
	}

	for {
		for j := 0; j < n; j++ {
			if p.done[j] || p.root[j] != j || p.bound[j] >= 0 {
				continue
			}
			if err := r.probeStep(ctx, p, j); err != nil {
				return 0, err
			}
		}
		r0 := p.findRoot(0)
		joined := true
		for j := 1; j < n; j++ {
			if p.findRoot(j) != r0 {
				joined = false
				break
			}
		}
		if joined && !p.done[r0] && p.bound[r0] < 0 {
			break
		}
		active := false
		for j := 0; j < n; j++ {
			if p.root[j] == j && !p.done[j] && p.bound[j] < 0 {
				active = true
			}
		}
		if !active {
			break
		}
	}

	for j := 0; j < n; j++ {
		rt := p.findRoot(j)
		for _, x := range p.path[j] {
			if p.done[rt] {
				r.firstTrusted[x] = p.res[rt]
			} else if p.pos[rt] != "" && p.pos[rt] != x {
				r.probeSkip[x] = p.pos[rt]
			}
		}
	}

	best := 0
	var bestAt *time.Time
	for j := 0; j < n; j++ {
		rt := p.findRoot(j)
		var at *time.Time
		if p.done[rt] {
			at = p.res[rt].at
		}
		if j == 0 {
			bestAt = at
			continue
		}
		if at != nil && (bestAt == nil || at.After(*bestAt)) {
			best, bestAt = j, at
		}
	}
	return best, nil
}

// probeStep moves probe j by one commit (or jumps along cached stop points) and settles it when it
// finds a trusted state, a cached answer, another probe's path, an ancestor stop or the floor.
func (r *landedRun) probeStep(ctx context.Context, p *probeState, j int) error {
	cur := p.pos[j]
	for cur != "" {
		if _, ok := r.firstTrusted[cur]; ok {
			break
		}
		next, ok := r.probeSkip[cur]
		if !ok {
			break
		}
		if _, seen := p.visited[cur]; seen {
			break
		}
		p.visited[cur] = j
		p.path[j] = append(p.path[j], cur)
		cur = next
	}
	p.pos[j] = cur
	if cur == "" {
		p.done[j] = true
		return nil
	}
	if hit, ok := r.firstTrusted[cur]; ok {
		p.res[j], p.done[j] = hit, true
		return nil
	}
	if v, ok := p.visited[cur]; ok {
		if k := p.findRoot(v); k != j {
			p.root[j] = k
		} else {
			r.warnf("commits: the parent probe reached %s twice; treating its line as having no trusted state", short(cur))
			p.done[j] = true
		}
		return nil
	}
	b, err := r.boundBy(ctx, p, j, cur)
	if err != nil {
		return err
	}
	if b >= 0 {
		p.bound[j] = b
		return nil
	}
	below, err := r.belowActivityFloor(ctx, cur)
	if err != nil {
		return err
	}
	p.visited[cur] = j
	p.path[j] = append(p.path[j], cur)
	if below {
		p.done[j] = true
		return nil
	}
	t, err := r.trust(ctx, cur, true)
	if err != nil {
		return err
	}
	if t.kind != trustNone {
		at := t.at
		p.res[j], p.done[j] = probeHit{at: &at, oid: cur}, true
		return nil
	}
	cm, err := r.api.commit(ctx, cur)
	if err != nil {
		return err
	}
	p.pos[j] = ""
	if len(cm.Parents) > 0 {
		p.pos[j] = cm.Parents[0]
	}
	return nil
}

// boundBy is the index of a settled probe whose first trusted state A has cur as an ancestor (cur
// is outside compare A...parent_j), or -1.
func (r *landedRun) boundBy(ctx context.Context, p *probeState, j int, cur string) (int, error) {
	for i := range p.parents {
		if i == j || p.root[i] != i || !p.done[i] || p.res[i].oid == "" {
			continue
		}
		ahead, ok, err := r.aheadSet(ctx, p.res[i].oid, p.parents[j])
		if err != nil {
			return -1, err
		}
		if ok && !ahead[cur] {
			return i, nil
		}
	}
	return -1, nil
}

type aheadEntry struct {
	set map[string]bool
	ok  bool
}

// aheadSet is the commits reachable from head and not from base (compare base...head), cached for
// the run. ok is false when compare cannot give the whole set (unrelated histories, or more than
// the 10,000 commits compare returns): the probe then keeps walking (warning).
func (r *landedRun) aheadSet(ctx context.Context, base, head string) (map[string]bool, bool, error) {
	key := base + "..." + head
	if e, ok := r.aheadSets[key]; ok {
		return e.set, e.ok, nil
	}
	commits, ahead, noCommon, err := r.compareAll(ctx, base, head)
	if err != nil {
		return nil, false, err
	}
	usable := !noCommon
	set := map[string]bool{}
	for _, cm := range commits {
		if sha, ok := cm["sha"].(string); ok {
			set[sha] = true
		}
	}
	if usable && len(set) < ahead {
		r.warnf("commits: compare %s...%s has more commits than GitHub returns (%d of %d); the parent probe keeps walking instead of stopping early", short(base), short(head), len(set), ahead)
		usable = false
	}
	r.aheadSets[key] = aheadEntry{set: set, ok: usable}
	return set, usable, nil
}

// prepareActivityFloor: in a repository with no merged PR, if B's activity log starts more
// than ciRetention ago, the commit B was at before its first entry is the floor: below it nothing
// has a PR, an activity entry or a CI record left.
func (r *landedRun) prepareActivityFloor(ctx context.Context) {
	if r.mergedPRs != 0 || !r.activityOK {
		return
	}
	first, err := r.api.oldestActivity(ctx, "refs/heads/"+r.branch)
	if err != nil {
		r.warnf("commits: first activity log entry not readable (%v); parent probes walk to their first trusted state", err)
		return
	}
	if first == nil || first.Before == "" || first.Before == zeroSHA || !first.Timestamp.Before(r.now.Add(-ciRetention)) {
		return
	}
	r.floor, r.floorAt = first.Before, first.Timestamp
	config.Debug("[GITHUB] no merged PRs; activity starts %s at %s: probes stop at its ancestors", fmtT(&r.floorAt), short(r.floor))
}

// belowActivityFloor: cur is an ancestor of the floor. Only commits dated before the activity log
// starts are asked (one compare each, cached); the answer itself comes from compare, not the date.
func (r *landedRun) belowActivityFloor(ctx context.Context, cur string) (bool, error) {
	if r.floor == "" {
		return false, nil
	}
	if v, ok := r.belowFloor[cur]; ok {
		return v, nil
	}
	c, err := r.api.commit(ctx, cur)
	if err != nil {
		return false, err
	}
	if !c.CommittedDate.Before(r.floorAt) {
		return false, nil
	}
	st, err := r.status(ctx, cur, r.floor)
	if err != nil {
		return false, err
	}
	r.belowFloor[cur] = st == "ahead" || st == "identical"
	return r.belowFloor[cur], nil
}
