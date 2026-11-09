package github

import (
	"fmt"
	"sort"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

// Runtime time invariants (PLAN §9, ported from w1_full_checks.py): I8 row sanity, I3 one push one
// time, I4 chain order, I1 ancestry, I2 single head (full coverage), I7 server-clock floor. A
// violation never drops rows: the run warns and the affected rows lose landed_at_exact and get a
// window just wide enough to hold the conflicting time. Differences within invariantTolerance
// (timestamp granularity) are "near" and ignored; I2 conflicts next to a force push are only
// reported ("investigate").

type checkClass int

const (
	checkOK checkClass = iota
	checkNear
	checkViolation
	checkInvestigate
)

func classify(delta time.Duration) checkClass {
	if delta <= 0 {
		return checkOK
	}
	if delta <= invariantTolerance {
		return checkNear
	}
	return checkViolation
}

type invariantChecker struct {
	r       *landedRun
	all     []*landedRow
	onB     map[string]*landedRow
	counts  map[string]int
	runEnd  time.Time
	forceAt []time.Time
}

func (r *landedRun) checkTimeInvariants(rows []*landedRow) {
	c := &invariantChecker{r: r, all: rows, onB: map[string]*landedRow{}, counts: map[string]int{}, runEnd: time.Now().UTC()}
	if r.now.After(c.runEnd) {
		c.runEnd = r.now
	}
	for _, row := range rows {
		if row.removedAt == nil {
			if _, ok := c.onB[row.sha]; !ok {
				c.onB[row.sha] = row
			}
		}
	}
	for _, e := range r.forcePushes(nil) {
		c.forceAt = append(c.forceAt, e.Timestamp)
	}
	c.checkI8()
	byStep := c.checkI3()
	c.checkI4(byStep)
	c.checkI1()
	c.checkI2()
	c.checkI7()
	if len(c.counts) > 0 {
		r.warnf("commits: time invariant violations (rows kept, widened, landed_at_exact=false): %v", c.counts)
	}
}

func (c *invariantChecker) note(inv string, cls checkClass, format string, args ...interface{}) {
	switch cls {
	case checkViolation:
		c.counts[inv]++
		c.r.warnf("commits: %s violation: %s", inv, fmt.Sprintf(format, args...))
	case checkInvestigate:
		c.r.warnf("commits: %s investigate (force push nearby): %s", inv, fmt.Sprintf(format, args...))
	case checkNear:
		config.Debug("[GITHUB] %s near (<= %v): %s", inv, invariantTolerance, fmt.Sprintf(format, args...))
	}
}

func widen(row *landedRow, inv string, lo, hi *time.Time) {
	row.exact = false
	if lo != nil && row.min != nil && lo.Before(*row.min) {
		l := *lo
		row.min = &l
	}
	if hi != nil && hi.After(row.max) {
		row.max = *hi
	}
	if row.min != nil && row.landed.Before(*row.min) {
		row.landed = *row.min
	}
	if row.landed.After(row.max) {
		row.landed = row.max
	}
	if !containsString(row.widened, inv) {
		row.widened = append(row.widened, inv)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func fmtT(t *time.Time) string {
	if t == nil {
		return "-inf"
	}
	return t.Format(time.RFC3339)
}

func (c *invariantChecker) checkI8() {
	for _, row := range c.all {
		if row.min != nil && row.min.After(row.landed) {
			c.note("I8", checkViolation, "%s min %s > landed %s (%s)", short(row.sha), fmtT(row.min), fmtT(&row.landed), row.source)
			widen(row, "I8", timePtr(row.landed), nil)
		}
		if row.landed.After(row.max) {
			c.note("I8", checkViolation, "%s landed %s > max %s (%s)", short(row.sha), fmtT(&row.landed), fmtT(&row.max), row.source)
			widen(row, "I8", nil, timePtr(row.landed))
		}
		if row.exact && (row.min == nil || !row.min.Equal(row.max)) {
			c.note("I8", checkViolation, "%s exact but min %s != max %s (%s)", short(row.sha), fmtT(row.min), fmtT(&row.max), row.source)
			widen(row, "I8", nil, nil)
		}
		if row.max.After(c.runEnd.Add(5 * time.Second)) {
			c.note("I8", checkViolation, "%s max %s after the run end %s", short(row.sha), fmtT(&row.max), fmtT(&c.runEnd))
		}
	}
}

func sameBound(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return absDuration(a.Sub(*b)) <= invariantTolerance
}

func (c *invariantChecker) checkI3() map[string][]*landedRow {
	byStep := map[string][]*landedRow{}
	var stepOrder []string
	for _, row := range c.all {
		if row.removedAt != nil || c.onB[row.sha] != row {
			continue
		}
		if _, ok := byStep[row.step]; !ok {
			stepOrder = append(stepOrder, row.step)
		}
		byStep[row.step] = append(byStep[row.step], row)
	}
	for _, step := range stepOrder {
		rows := byStep[step]
		var own *landedRow
		var others []*landedRow
		lo, hi := rows[0].landed, rows[0].landed
		for _, row := range rows {
			if row.sha == step {
				own = row
			} else {
				others = append(others, row)
			}
			if row.landed.Before(lo) {
				lo = row.landed
			}
			if row.landed.After(hi) {
				hi = row.landed
			}
		}
		var bad []string
		if hi.Sub(lo) > invariantTolerance {
			bad = append(bad, fmt.Sprintf("landed_at spread %v", hi.Sub(lo)))
		}
		if len(others) > 0 {
			o := others[0]
			for _, x := range others[1:] {
				if !sameBound(x.min, o.min) || absDuration(x.max.Sub(o.max)) > invariantTolerance {
					bad = append(bad, "non-step rows have different windows")
					break
				}
			}
			if own != nil {
				if own.exact {
					d := own.landed.Sub(o.max)
					if o.min != nil && o.min.Sub(own.landed) > d {
						d = o.min.Sub(own.landed)
					}
					if d > invariantTolerance {
						bad = append(bad, fmt.Sprintf("step time %s outside its commits' window (%s, %s]", fmtT(&own.landed), fmtT(o.min), fmtT(&o.max)))
					}
				} else if !sameExactBound(own.min, o.min) || !own.max.Equal(o.max) {
					bad = append(bad, "non-exact step window differs from its commits'")
				}
			}
		}
		if len(bad) == 0 {
			continue
		}
		c.note("I3", checkViolation, "step %s: %v", short(step), bad)
		var wlo *time.Time
		whi := rows[0].max
		unbounded := false
		for _, row := range rows {
			if row.min == nil {
				unbounded = true
			} else if wlo == nil || row.min.Before(*wlo) {
				wlo = timePtr(*row.min)
			}
			if row.max.After(whi) {
				whi = row.max
			}
		}
		for _, row := range rows {
			if unbounded {
				widen(row, "I3", nil, timePtr(whi))
				row.min = nil
			} else {
				widen(row, "I3", wlo, timePtr(whi))
			}
		}
	}
	return byStep
}

func sameExactBound(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (c *invariantChecker) checkI4(byStep map[string][]*landedRow) {
	var last *chainStep
	for _, st := range c.r.steps {
		if st.trust.kind != trustFull {
			continue
		}
		if last != nil {
			cls := classify(last.trust.at.Sub(st.trust.at))
			if cls != checkOK {
				c.note("I4", cls, "trusted time decreases along the chain: %s (%s) %s -> %s (%s) %s", short(last.oid), last.trust.source, fmtT(&last.trust.at), short(st.oid), st.trust.source, fmtT(&st.trust.at))
			}
			if cls == checkViolation {
				for _, row := range append(append([]*landedRow(nil), byStep[last.oid]...), byStep[st.oid]...) {
					widen(row, "I4", timePtr(st.trust.at), timePtr(last.trust.at))
				}
			}
		}
		last = st
	}
}

func (c *invariantChecker) sortedOnB() []string {
	shas := make([]string, 0, len(c.onB))
	for sha := range c.onB {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	return shas
}

func (c *invariantChecker) topo() []string {
	indeg := map[string]int{}
	children := map[string][]string{}
	shas := c.sortedOnB()
	for _, s := range shas {
		indeg[s] += 0
		for _, p := range c.onB[s].parents {
			if _, ok := c.onB[p]; ok {
				children[p] = append(children[p], s)
				indeg[s]++
			}
		}
	}
	var queue, order []string
	for _, s := range shas {
		if indeg[s] == 0 {
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		order = append(order, s)
		for _, x := range children[s] {
			indeg[x]--
			if indeg[x] == 0 {
				queue = append(queue, x)
			}
		}
	}
	return order
}

func (c *invariantChecker) checkI1() {
	type bestMin struct {
		at  *time.Time
		sha string
	}
	best := map[string]bestMin{}
	later := func(a, b *time.Time) bool { return a != nil && (b == nil || a.After(*b)) }
	for _, s := range c.topo() {
		b := bestMin{}
		for _, p := range c.onB[s].parents {
			pr, ok := c.onB[p]
			if !ok {
				continue
			}
			if later(pr.min, b.at) {
				b = bestMin{at: pr.min, sha: p}
			}
			if later(best[p].at, b.at) {
				b = best[p]
			}
		}
		best[s] = b
		if b.sha == "" || b.at == nil {
			continue
		}
		row := c.onB[s]
		cls := classify(b.at.Sub(row.max))
		if cls == checkOK {
			continue
		}
		anc := c.onB[b.sha]
		c.note("I1", cls, "ancestor %s min %s > descendant %s max %s (%s / %s)", short(b.sha), fmtT(b.at), short(s), fmtT(&row.max), anc.source, row.source)
		if cls == checkViolation {
			at := *b.at
			widen(anc, "I1", timePtr(row.max), nil)
			widen(row, "I1", nil, &at)
		}
	}
}

func (c *invariantChecker) forceNear(a, b time.Time) bool {
	lo, hi := a, b
	if hi.Before(lo) {
		lo, hi = hi, lo
	}
	lo, hi = lo.Add(-invariantTolerance), hi.Add(invariantTolerance)
	for _, f := range c.forceAt {
		if !f.Before(lo) && !f.After(hi) {
			return true
		}
	}
	return false
}

// checkI2: for every exact landing time t, the rows landed by t must be exactly the ancestors of one
// head; heads at successive times must be nested. Every row (exact or window) is checked against
// the head sets (full coverage, Kerem 8 Nov).
func (c *invariantChecker) checkI2() {
	var exact []*landedRow
	byT := map[time.Time][]string{}
	for _, s := range c.sortedOnB() {
		row := c.onB[s]
		if row.exact {
			exact = append(exact, row)
			byT[row.landed] = append(byT[row.landed], s)
		}
	}
	times := make([]time.Time, 0, len(byT))
	for t := range byT {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	if len(times) == 0 {
		return
	}

	mark := map[string]int{}
	heads := make([]string, 0, len(times))
	type event struct {
		multi bool
		k     int
		heads []string
	}
	var events []event
	for k, t := range times {
		var members []string
		for _, m := range byT[t] {
			if _, ok := mark[m]; !ok {
				members = append(members, m)
			}
		}
		if len(members) == 0 {
			heads = append(heads, "")
			continue
		}
		mset := map[string]bool{}
		reached := map[string]bool{}
		touched := map[string]bool{}
		queue := append([]string(nil), members...)
		for _, m := range members {
			mset[m] = true
			mark[m] = k
		}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, p := range c.onB[x].parents {
				if _, ok := c.onB[p]; !ok {
					continue
				}
				if mk, ok := mark[p]; ok {
					if mk == k {
						if mset[p] {
							reached[p] = true
						}
					} else {
						touched[p] = true
					}
					continue
				}
				mark[p] = k
				queue = append(queue, p)
			}
		}
		var maximal []string
		for _, m := range members {
			if !reached[m] {
				maximal = append(maximal, m)
			}
		}
		if len(maximal) == 0 {
			maximal = members[:1]
		}
		if len(maximal) > 1 {
			events = append(events, event{multi: true, k: k, heads: maximal})
		}
		h := maximal[0]
		prev := ""
		for j := k - 1; j >= 0; j-- {
			if heads[j] != "" {
				prev = heads[j]
				break
			}
		}
		if prev != "" && !touched[prev] && prev != h {
			events = append(events, event{k: k, heads: []string{prev, h}})
		}
		heads = append(heads, h)
	}

	ancestors := map[string]map[string]bool{}
	ancOf := func(x string) map[string]bool {
		if a, ok := ancestors[x]; ok {
			return a
		}
		seen := map[string]bool{x: true}
		queue := []string{x}
		for len(queue) > 0 {
			y := queue[0]
			queue = queue[1:]
			for _, p := range c.onB[y].parents {
				if _, ok := c.onB[p]; ok && !seen[p] {
					seen[p] = true
					queue = append(queue, p)
				}
			}
		}
		ancestors[x] = seen
		return seen
	}
	joinedWithinTol := func(t time.Time, hs []string) bool {
		for _, row := range exact {
			if !row.landed.After(t) || row.landed.After(t.Add(invariantTolerance)) {
				continue
			}
			all := true
			for _, h := range hs {
				if !ancOf(row.sha)[h] {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
		return false
	}

	for _, ev := range events {
		t := times[ev.k]
		if ev.multi {
			cls := checkViolation
			if c.forceNear(t, t) {
				cls = checkInvestigate
			} else if joinedWithinTol(t, ev.heads) {
				cls = checkNear
			}
			c.note("I2", cls, "%d incomparable exact heads at %s: %v", len(ev.heads), fmtT(&t), ev.heads)
			if cls == checkViolation {
				var lo *time.Time
				if ev.k > 0 {
					lo = timePtr(times[ev.k-1])
				}
				for _, h := range ev.heads {
					widen(c.onB[h], "I2", lo, nil)
				}
			}
			continue
		}
		prev, h := ev.heads[0], ev.heads[1]
		tprev := c.onB[prev].landed
		cls := checkViolation
		if c.forceNear(tprev, t) {
			cls = checkInvestigate
		} else if t.Sub(tprev) <= invariantTolerance || joinedWithinTol(t, ev.heads) {
			cls = checkNear
		}
		c.note("I2", cls, "head %s at %s is not a descendant of the previous exact head %s at %s", short(h), fmtT(&t), short(prev), fmtT(&tprev))
		if cls == checkViolation {
			widen(c.onB[h], "I2", timePtr(tprev), nil)
			widen(c.onB[prev], "I2", nil, timePtr(t))
		}
	}

	for _, s := range c.sortedOnB() {
		row := c.onB[s]
		loV, hiV := row.min, row.max
		if row.exact {
			loV, hiV = timePtr(row.landed), row.landed
		}
		var prevT *time.Time
		if k, ok := mark[s]; ok {
			if loV != nil {
				cls := classify(loV.Sub(times[k]))
				if cls != checkOK && c.forceNear(times[k], *loV) {
					cls = checkInvestigate
				}
				c.note("I2", cls, "%s is in the head set at %s but its min/landed %s is later (%s via %s)", short(s), fmtT(&times[k]), fmtT(loV), row.source, short(row.step))
				if cls == checkViolation {
					widen(row, "I2", timePtr(times[k]), nil)
				}
			}
			if k > 0 {
				prevT = timePtr(times[k-1])
			}
		} else {
			prevT = timePtr(times[len(times)-1])
		}
		if prevT != nil {
			cls := classify(prevT.Sub(hiV))
			if cls != checkOK && c.forceNear(*prevT, hiV) {
				cls = checkInvestigate
			}
			c.note("I2", cls, "%s is not in the head set at %s but its max/landed %s is earlier (%s via %s)", short(s), fmtT(prevT), fmtT(&hiV), row.source, short(row.step))
			if cls == checkViolation {
				widen(row, "I2", nil, prevT)
			}
		}
	}
}

func (c *invariantChecker) checkI7() {
	for _, row := range c.all {
		if !row.signed || row.committedAt == nil {
			continue
		}
		hi := row.max
		if row.exact {
			hi = row.landed
		}
		cls := classify(row.committedAt.Sub(hi))
		c.note("I7", cls, "%s GitHub-signed, committedDate %s > %s (%s)", short(row.sha), fmtT(row.committedAt), fmtT(&hi), row.source)
		if cls == checkViolation {
			widen(row, "I7", nil, timePtr(*row.committedAt))
		}
	}
}
