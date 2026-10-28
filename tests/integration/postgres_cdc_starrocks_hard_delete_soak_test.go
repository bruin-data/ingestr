//go:build stress

// PostgreSQL CDC → StarRocks soak with --cdc-delete-behavior=hard. A large
// seeded source takes a sustained mixed workload (single-row and multi-row
// transactions, delete-then-reinsert, insert-then-delete, primary-key moves,
// periodic bulk range deletes and updates) while the real ingestr binary
// streams into StarRocks. A steady phase measures clean latencies; a chaos
// phase adds SIGKILL/SIGTERM restarts, schema changes (including a StarRocks
// conversion job, during which ingestr is killed on purpose), and a late
// table. Probes commit individual operations on a dedicated table and poll
// StarRocks until each is visible, recording end-to-end latency per operation.
// The run ends with value-by-value verification and a report of latency
// distributions and resource usage written to SOAK_RESULTS_DIR.
//
//	make build && go test -tags stress -count=1 -v -timeout 180m \
//	  -run '^TestPostgresCDC_StarRocksHardDeleteSoak$' ./tests/integration/
package integration

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var (
	soakTables       = envInt("SOAK_TABLES", 4)
	soakSeedRows     = envInt("SOAK_SEED_ROWS", 2_000_000)
	soakEvolvingRows = envInt("SOAK_EVOLVING_ROWS", 2_000_000)
	soakLateRows     = envInt("SOAK_LATE_ROWS", 300_000)
	soakOpsPerSec    = envInt("SOAK_OPS_PER_SEC", 3000)
	soakWorkers      = envInt("SOAK_WORKERS", 48)
	soakSteady       = envDuration("SOAK_STEADY", 10*time.Minute)
	soakChaos        = soakOptionalDuration("SOAK_CHAOS", 10*time.Minute)
	soakHardDelete   = os.Getenv("SOAK_DELETE_BEHAVIOR") != "soft"
	soakProbeRate    = float64(envInt("SOAK_PROBE_RATE", 4))
	soakBulkEvery    = envDuration("SOAK_BULK_EVERY", 20*time.Second)
	soakSampleEvery  = envDuration("SOAK_SAMPLE_EVERY", 5*time.Second)
	soakPollEvery    = envDuration("SOAK_POLL_EVERY", 25*time.Millisecond)
	soakFlushRecords = envInt("SOAK_FLUSH_RECORDS", 25000)
)

// soakOptionalDuration is envDuration that also accepts 0 to skip a phase.
func soakOptionalDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return def
}

const (
	soakProbeTable  = "soak_probe"
	soakMetricsAddr = "127.0.0.1:19464"
)

type soakTable struct {
	name   string
	rich   bool
	nextID atomic.Int64
	simple *stressTable
}

func soakCreateRich(ctx context.Context, pool *pgxpool.Pool, name string, rows int) error {
	_, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%[1]s (
			id BIGINT PRIMARY KEY,
			val BIGINT NOT NULL,
			amount NUMERIC(18,4) NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL,
			doc JSONB,
			updated_at TIMESTAMPTZ NOT NULL
		);
		INSERT INTO public.%[1]s
		SELECT g, g, g / 7.0, (ARRAY['new','active','paused','closed'])[1 + g %% 4],
			md5(g::text) || repeat('p', 160), jsonb_build_object('g', g, 'tags', jsonb_build_array('a', g %% 10)), now()
		FROM generate_series(1, %[2]d) g;
	`, name, rows))
	return err
}

// soakOps tracks every mutation kind the workload commits.
type soakOps struct {
	mu     sync.Mutex
	counts map[string]int64
	rows   map[string]int64
}

func (o *soakOps) add(kind string, rows int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.counts[kind]++
	o.rows[kind] += rows
}

// soakRichOp runs one rich-table operation and returns its kind, the operation
// kind reported to the shared load generator, and rows affected.
func soakRichOp(ctx context.Context, pool *pgxpool.Pool, rng *rand.Rand, tbl *soakTable) (string, string, int64, error) {
	t := "public." + tbl.name
	randomID := func() int64 { return rng.Int63n(tbl.nextID.Load()) + 1 }
	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, val, amount, status, payload, doc, updated_at) VALUES ($1, $2, $3, $4, $5, $6, now())`, t)
	values := func(id int64) []any {
		return []any{
			id, rng.Int63n(1 << 40), fmt.Sprintf("%d.%04d", rng.Intn(1_000_000), rng.Intn(10_000)),
			[]string{"new", "active", "paused", "closed"}[rng.Intn(4)], fmt.Sprintf("ins-%d-%x", id, rng.Int63()),
			fmt.Sprintf(`{"g": %d, "r": %d, "tags": ["b", %d]}`, id, rng.Intn(1000), rng.Intn(10)),
		}
	}
	updateSQL := fmt.Sprintf(`UPDATE %s SET val = val + 1, amount = amount + 0.5, status = $2, payload = $3, doc = jsonb_set(COALESCE(doc, '{}'), '{u}', to_jsonb($4::int)), updated_at = now() WHERE id = $1`, t)
	deleteSQL := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, t)
	switch roll := rng.Intn(100); {
	case roll < 30:
		id := tbl.nextID.Add(1)
		_, err := pool.Exec(ctx, insertSQL, values(id)...)
		return "insert", "insert", 1, err
	case roll < 55:
		tag, err := pool.Exec(ctx, updateSQL, randomID(), "upd", fmt.Sprintf("upd-%x", rng.Int63()), rng.Intn(1000))
		return "update", "update", tag.RowsAffected(), err
	case roll < 75:
		tag, err := pool.Exec(ctx, deleteSQL, randomID())
		return "delete", "delete", tag.RowsAffected(), err
	case roll < 83:
		// A key recreated after an earlier move would collide with the moved
		// row; that statement is simply a no-op.
		tag, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET id = id + $1, updated_at = now() WHERE id = $2 AND id < $1`, t), stressPKMoveOffset, randomID())
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "pk-move (collision, no-op)", "pk-update", 0, nil
		}
		return "pk-move", "pk-update", tag.RowsAffected(), err
	case roll < 90:
		// The same key is deleted and recreated in one transaction: the
		// destination must end with the new row, not a deletion.
		id := randomID()
		var affected int64
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, deleteSQL, id)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
			_, err = tx.Exec(ctx, insertSQL, values(id)...)
			return err
		})
		return "delete-reinsert", "delete", affected, err
	case roll < 95:
		// A key that never survives a transaction must never appear.
		id := tbl.nextID.Add(1)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, insertSQL, values(id)...); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, updateSQL, id, "transient", "transient", 1)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, deleteSQL, id)
			return err
		})
		return "insert-update-delete", "delete", 1, err
	default:
		var affected int64
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			for range 2 {
				tag, err := tx.Exec(ctx, updateSQL, randomID(), "multi", fmt.Sprintf("multi-%x", rng.Int63()), rng.Intn(1000))
				if err != nil {
					return err
				}
				affected += tag.RowsAffected()
			}
			tag, err := tx.Exec(ctx, deleteSQL, randomID())
			affected += tag.RowsAffected()
			return err
		})
		return "multi-row-txn", "update", affected, err
	}
}

func soakSimpleOp(ctx context.Context, pool *pgxpool.Pool, rng *rand.Rand, tbl *stressTable) (string, string, int64, error) {
	switch roll := rng.Intn(100); {
	case roll < 35:
		id := tbl.nextID.Add(1)
		_, err := pool.Exec(ctx, tbl.insertSQL, id, id, fmt.Sprintf("ins-%d", id))
		return "insert", "insert", 1, err
	case roll < 70:
		tag, err := pool.Exec(ctx, tbl.updateSQL, rng.Int63n(tbl.nextID.Load())+1, fmt.Sprintf("upd-%x", rng.Int63()))
		return "update", "update", tag.RowsAffected(), err
	case roll < 92:
		tag, err := pool.Exec(ctx, tbl.deleteSQL, rng.Int63n(tbl.nextID.Load())+1)
		return "delete", "delete", tag.RowsAffected(), err
	default:
		tag, err := pool.Exec(ctx, tbl.pkUpdateSQL, stressPKMoveOffset, rng.Int63n(tbl.nextID.Load())+1)
		return "pk-move", "pk-update", tag.RowsAffected(), err
	}
}

// --- latency probes ---

type soakSample struct {
	Phase   string        `json:"phase"`
	Op      string        `json:"op"`
	At      time.Time     `json:"at"`
	Latency time.Duration `json:"latency_ns"`
}

type soakProbeWait struct {
	seq       int64
	delete    bool
	logicalAt time.Time
	done      chan [2]time.Time
}

type soakProbes struct {
	mu      sync.Mutex
	pending map[int64]*soakProbeWait
	samples []soakSample
	polls   atomic.Int64
	pollErr atomic.Int64
	// soft resolves a delete once its tombstone is visible.
	soft bool
	// deletes counts resolved delete probes; tombstoneSeen counts those
	// whose tombstone was caught by at least one poll before the row vanished.
	deletes, tombstoneSeen atomic.Int64
}

func (p *soakProbes) wait(id, seq int64, del bool) <-chan [2]time.Time {
	w := &soakProbeWait{seq: seq, delete: del, done: make(chan [2]time.Time, 1)}
	p.mu.Lock()
	p.pending[id] = w
	p.mu.Unlock()
	return w.done
}

func (p *soakProbes) record(phase, op string, at time.Time, latency time.Duration) {
	p.mu.Lock()
	p.samples = append(p.samples, soakSample{Phase: phase, Op: op, At: at, Latency: latency})
	p.mu.Unlock()
}

// poll resolves pending probes against StarRocks. The observation time is the
// moment the query was issued, so a probe's latency never includes the poll's
// own round trip.
func (p *soakProbes) poll(ctx context.Context, sr *sql.DB) {
	ticker := time.NewTicker(soakPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		p.mu.Lock()
		ids := make([]string, 0, len(p.pending))
		queried := make(map[int64]*soakProbeWait, len(p.pending))
		for id, w := range p.pending {
			ids = append(ids, strconv.FormatInt(id, 10))
			queried[id] = w
		}
		p.mu.Unlock()
		if len(ids) == 0 {
			continue
		}
		observed := time.Now()
		rows, err := sr.QueryContext(ctx, fmt.Sprintf("SELECT id, seq, COALESCE(_cdc_deleted, false) FROM %s.%s WHERE id IN (%s)", srStressDatabase, soakProbeTable, strings.Join(ids, ",")))
		if err != nil {
			p.pollErr.Add(1)
			continue
		}
		type state struct {
			seq     int64
			deleted bool
		}
		present := map[int64]state{}
		for rows.Next() {
			var id int64
			var s state
			if rows.Scan(&id, &s.seq, &s.deleted) == nil {
				present[id] = s
			}
		}
		_ = rows.Close()
		p.polls.Add(1)
		p.mu.Lock()
		for id, w := range p.pending {
			// Only waits registered before this poll's query can be judged by it.
			if queried[id] != w {
				continue
			}
			s, ok := present[id]
			switch {
			case !w.delete && ok && !s.deleted && s.seq == w.seq:
				w.done <- [2]time.Time{observed, observed}
				delete(p.pending, id)
			case w.delete && !ok:
				p.deletes.Add(1)
				if w.logicalAt.IsZero() {
					w.logicalAt = observed
				} else {
					p.tombstoneSeen.Add(1)
				}
				w.done <- [2]time.Time{w.logicalAt, observed}
				delete(p.pending, id)
			case w.delete && s.deleted && p.soft:
				p.deletes.Add(1)
				p.tombstoneSeen.Add(1)
				w.done <- [2]time.Time{observed, observed}
				delete(p.pending, id)
			case w.delete && s.deleted && w.logicalAt.IsZero():
				w.logicalAt = observed
			}
		}
		p.mu.Unlock()
	}
}

func (p *soakProbes) outstanding() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}

// lifecycle drives one probe key through insert → update → update → delete,
// waiting for each operation to become visible in StarRocks before committing
// the next one after a random pause, so commits land at arbitrary points of
// the flush cycle instead of right after the previous flush.
func (p *soakProbes) lifecycle(pool *pgxpool.Pool, id int64, rng *rand.Rand, phase *atomic.Value, fail func(error)) {
	var seq int64
	step := func(op string, del bool, statement string, args ...any) bool {
		seq++
		ch := p.wait(id, seq, del)
		if _, err := pool.Exec(context.Background(), statement, args...); err != nil {
			p.mu.Lock()
			delete(p.pending, id)
			p.mu.Unlock()
			fail(fmt.Errorf("probe %s id=%d: %w", op, id, err))
			return false
		}
		committed := time.Now()
		currentPhase := phase.Load().(string)
		select {
		case seen := <-ch:
			if del {
				p.record(currentPhase, "delete (tombstone or gone)", committed, seen[0].Sub(committed))
				if !p.soft {
					p.record(currentPhase, "delete (row gone)", committed, seen[1].Sub(committed))
					if seen[1].After(seen[0]) {
						p.record(currentPhase, "delete tombstone exposure", committed, seen[1].Sub(seen[0]))
					}
				}
			} else {
				p.record(currentPhase, op, committed, seen[0].Sub(committed))
			}
			time.Sleep(time.Duration(rng.Int63n(int64(5 * time.Second))))
			return true
		case <-time.After(time.Hour):
			fail(fmt.Errorf("probe %s id=%d not visible in StarRocks after 1h", op, id))
			return false
		}
	}
	table := "public." + soakProbeTable
	_ = step("insert", false, fmt.Sprintf("INSERT INTO %s (id, seq, note, updated_at) VALUES ($1, $2, 'probe', now())", table), id, seq+1) &&
		step("update", false, fmt.Sprintf("UPDATE %s SET seq = $2, updated_at = now() WHERE id = $1", table), id, seq+1) &&
		step("update", false, fmt.Sprintf("UPDATE %s SET seq = $2, note = 'probe-2', updated_at = now() WHERE id = $1", table), id, seq+1) &&
		step("delete", true, fmt.Sprintf("DELETE FROM %s WHERE id = $1", table), id)
}

// launch starts probe lifecycles as a Poisson process at soakProbeRate per
// second until ctx is done, and returns once every lifecycle has finished.
func (p *soakProbes) launch(ctx context.Context, pool *pgxpool.Pool, phase *atomic.Value, fail func(error)) {
	rng := rand.New(rand.NewSource(7919))
	var wg sync.WaitGroup
	for id := int64(1_000_000_000_000); ; id++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-time.After(time.Duration(rng.ExpFloat64() / soakProbeRate * float64(time.Second))):
		}
		wg.Add(1)
		go func(id, seed int64) {
			defer wg.Done()
			p.lifecycle(pool, id, rand.New(rand.NewSource(seed)), phase, fail)
		}(id, rng.Int63())
	}
}

// --- resource sampling ---

type soakResourceSample struct {
	At                time.Time `json:"at"`
	Phase             string    `json:"phase"`
	IngestrCPU        float64   `json:"ingestr_cpu_pct"`
	IngestrRSSMB      float64   `json:"ingestr_rss_mb"`
	PGCPU             float64   `json:"pg_cpu_pct"`
	PGMemMB           float64   `json:"pg_mem_mb"`
	SRCPU             float64   `json:"sr_cpu_pct"`
	SRMemMB           float64   `json:"sr_mem_mb"`
	PGCommitsPerSec   float64   `json:"pg_commits_per_sec"`
	PGRowChangesPerS  float64   `json:"pg_row_changes_per_sec"`
	WALBytesPerSec    float64   `json:"wal_bytes_per_sec"`
	SlotLagBytes      float64   `json:"slot_lag_bytes"`
	RowsSyncedPerSec  float64   `json:"rows_synced_per_sec"`
	SecondsBehind     float64   `json:"seconds_behind"`
	LiveTombstones    int64     `json:"live_tombstones"`
	HostLoad1         float64   `json:"host_load1"`
	rowsSyncedCounter float64
}

func soakDockerStats(names ...string) map[string][2]float64 {
	out, err := exec.Command("docker", append([]string{"stats", "--no-stream", "--format", "{{.Name}}|{{.ID}}|{{.CPUPerc}}|{{.MemUsage}}"}, names...)...).Output()
	result := map[string][2]float64{}
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64)
		mem := soakParseBytes(strings.TrimSpace(strings.Split(parts[3], "/")[0])) / (1 << 20)
		result[parts[0]] = [2]float64{cpu, mem}
		result[parts[1]] = [2]float64{cpu, mem}
	}
	return result
}

func soakParseBytes(s string) float64 {
	units := []struct {
		suffix string
		mult   float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			v, _ := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			return v * u.mult
		}
	}
	return 0
}

func soakIngestrProcess(bin string) (cpu, rssMB float64) {
	out, err := exec.Command("ps", "-axo", "pcpu=,rss=,command=").Output()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, bin+" ingest") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		c, _ := strconv.ParseFloat(fields[0], 64)
		r, _ := strconv.ParseFloat(fields[1], 64)
		cpu += c
		rssMB += r / 1024
	}
	return cpu, rssMB
}

func soakIngestrMetrics() map[string]float64 {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + soakMetricsAddr + "/metrics")
	result := map[string]float64{}
	if err != nil {
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		name := line
		if i := strings.IndexAny(line, "{ "); i > 0 {
			name = line[:i]
		}
		fields := strings.Fields(line)
		if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
			result[name] += v
		}
	}
	return result
}

// --- statistics ---

func soakPercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[max(0, min(idx, len(sorted)-1))]
}

func soakSecs(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }

func soakLatencyTable(samples []soakSample, phases []string) string {
	var b strings.Builder
	b.WriteString("| phase | operation | n | mean s | p50 s | p90 s | p95 s | p99 s | p99.9 s | max s |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	ops := []string{"insert", "update", "delete (tombstone or gone)", "delete (row gone)", "delete tombstone exposure"}
	for _, phase := range phases {
		for _, op := range ops {
			var ds []time.Duration
			var total time.Duration
			for _, s := range samples {
				if s.Op == op && (phase == "all" || s.Phase == phase) {
					ds = append(ds, s.Latency)
					total += s.Latency
				}
			}
			if len(ds) == 0 {
				continue
			}
			slices.Sort(ds)
			fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %s | %s | %s | %s |\n", phase, op, len(ds),
				soakSecs(total/time.Duration(len(ds))), soakSecs(soakPercentile(ds, 50)), soakSecs(soakPercentile(ds, 90)),
				soakSecs(soakPercentile(ds, 95)), soakSecs(soakPercentile(ds, 99)), soakSecs(soakPercentile(ds, 99.9)), soakSecs(ds[len(ds)-1]))
		}
	}
	return b.String()
}

func soakHistogram(samples []soakSample, phase, op string) string {
	bounds := []time.Duration{
		250 * time.Millisecond, 500 * time.Millisecond, 750 * time.Millisecond, time.Second, 1500 * time.Millisecond,
		2 * time.Second, 2500 * time.Millisecond, 3 * time.Second, 4 * time.Second, 5 * time.Second, 7500 * time.Millisecond, 10 * time.Second, 30 * time.Second, time.Minute,
	}
	counts := make([]int, len(bounds)+1)
	total := 0
	for _, s := range samples {
		if s.Op != op || s.Phase != phase {
			continue
		}
		total++
		i := sort.Search(len(bounds), func(i int) bool { return s.Latency < bounds[i] })
		counts[i]++
	}
	if total == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s / %s (n=%d)\n", phase, op, total)
	peak := slices.Max(counts)
	lower := time.Duration(0)
	for i, c := range counts {
		label := fmt.Sprintf("%5.2fs–%5.2fs", lower.Seconds(), 0.0)
		if i < len(bounds) {
			label = fmt.Sprintf("%5.2fs–%5.2fs", lower.Seconds(), bounds[i].Seconds())
			lower = bounds[i]
		} else {
			label = fmt.Sprintf("   ≥%5.2fs   ", lower.Seconds())
		}
		bar := strings.Repeat("█", int(math.Round(40*float64(c)/float64(max(peak, 1)))))
		fmt.Fprintf(&b, "  %s %6d %5.1f%% %s\n", label, c, 100*float64(c)/float64(total), bar)
	}
	return b.String()
}

func soakSummary(values []float64) string {
	if len(values) == 0 {
		return "n/a"
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	pct := func(p float64) float64 {
		return sorted[max(0, min(int(math.Ceil(p/100*float64(len(sorted))))-1, len(sorted)-1))]
	}
	return fmt.Sprintf("mean %.1f | p50 %.1f | p95 %.1f | max %.1f", sum/float64(len(sorted)), pct(50), pct(95), sorted[len(sorted)-1])
}

var soakFlushLine = regexp.MustCompile(`cycle \d+: flushed (\d+) rows in ([0-9.]+(?:ms|s|µs|m))`)

func TestPostgresCDC_StarRocksHardDeleteSoak(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping soak: Docker provider is not available/healthy")
	}
	bin := os.Getenv("STRESS_INGESTR_BIN")
	if bin == "" {
		bin = "../../bin/ingestr"
	}
	bin, err := filepath.Abs(bin)
	require.NoError(t, err)
	_, err = os.Stat(bin)
	require.NoError(t, err, "build the binary first with `make build` or set STRESS_INGESTR_BIN")
	resultsDir := os.Getenv("SOAK_RESULTS_DIR")
	if resultsDir == "" {
		resultsDir = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(resultsDir, 0o755))
	t.Logf("results: %s", resultsDir)

	sourceContainer, sourceConnString := stressSourceContainer(t, ctx)
	srDSN, srURI := srStressContainer(t, ctx)
	sr := srStressOpenDB(t, srDSN)
	sr.SetMaxOpenConns(16)

	srcPool, err := pgxpool.New(ctx, fmt.Sprintf("%s&pool_max_conns=%d", sourceConnString, soakWorkers+24))
	require.NoError(t, err)
	t.Cleanup(srcPool.Close)
	verifyPool, err := pgxpool.New(ctx, sourceConnString+"&timezone=UTC&pool_max_conns=8")
	require.NoError(t, err)
	t.Cleanup(verifyPool.Close)

	// --- seed ---
	seedStart := time.Now()
	var tables []*soakTable
	var tablesMu sync.Mutex
	var seedWG sync.WaitGroup
	seedErrs := make(chan error, soakTables+2)
	for i := 0; i < soakTables; i++ {
		tbl := &soakTable{name: fmt.Sprintf("soak_rich_%d", i), rich: true}
		tbl.nextID.Store(int64(soakSeedRows))
		tables = append(tables, tbl)
		seedWG.Add(1)
		go func() { defer seedWG.Done(); seedErrs <- soakCreateRich(ctx, srcPool, tbl.name, soakSeedRows) }()
	}
	seedWG.Add(1)
	go func() {
		defer seedWG.Done()
		seedErrs <- stressCreateEvolvingAndSeed(ctx, srcPool, soakEvolvingRows)
	}()
	seedWG.Wait()
	close(seedErrs)
	for err := range seedErrs {
		require.NoError(t, err)
	}
	evolving := &soakTable{name: stressEvolvingTable, simple: newStressTable(stressEvolvingTable, int64(soakEvolvingRows))}
	tables = append(tables, evolving)
	_, err = srcPool.Exec(ctx, fmt.Sprintf(`CREATE TABLE public.%s (id BIGINT PRIMARY KEY, seq BIGINT NOT NULL, note TEXT, updated_at TIMESTAMPTZ NOT NULL)`, soakProbeTable))
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `VACUUM ANALYZE`)
	require.NoError(t, err)
	seededRows := int64(soakTables*soakSeedRows + soakEvolvingRows)
	t.Logf("seeded %d rows across %d tables in %v", seededRows, len(tables), time.Since(seedStart).Round(time.Second))

	// --- ingestr ---
	destURI := strings.Replace(srURI, "/?", "/"+srStressDatabase+"?", 1)
	sink := &srStressSink{
		t:      t,
		bin:    bin,
		logDir: filepath.Join(resultsDir, "ingestr-logs"),
		args: []string{
			"ingest",
			"--source-uri", "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=5s&state_id=postgres-starrocks-hard-delete-soak&dest_schema=" + srStressDatabase,
			"--dest-uri", destURI,
			"--cdc-table-naming", "table",
			"--cdc-delete-behavior", map[bool]string{true: "hard", false: "soft"}[soakHardDelete],
			"--incremental-strategy", "merge",
			"--stream",
			"--flush-interval", srStressFlushInterval.String(),
			"--flush-records", strconv.Itoa(soakFlushRecords),
			"--metrics-addr", soakMetricsAddr,
			"--progress", "log",
			"--yes",
		},
	}
	require.NoError(t, os.MkdirAll(sink.logDir, 0o755))
	var targetedKill atomic.Bool
	sink.expectedExit = func() bool { return targetedKill.Swap(false) }
	sinkCtx, stopSink := context.WithCancel(ctx)
	sinkDone := make(chan struct{})
	snapshotStart := time.Now()
	go func() { defer close(sinkDone); sink.run(sinkCtx) }()
	t.Cleanup(func() { stopSink(); <-sinkDone })

	var phase atomic.Value
	phase.Store("snapshot")

	// --- resource sampler (runs for the whole test) ---
	srContainer := os.Getenv("SOAK_SR_CONTAINER")
	pgContainer := sourceContainer.GetContainerID()
	var resources []soakResourceSample
	var resourcesMu sync.Mutex
	samplerCtx, stopSampler := context.WithCancel(ctx)
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		var prev *soakResourceSample
		var prevCommits, prevChanges, prevWAL float64
		ticker := time.NewTicker(soakSampleEvery)
		defer ticker.Stop()
		for {
			select {
			case <-samplerCtx.Done():
				return
			case <-ticker.C:
			}
			s := soakResourceSample{At: time.Now(), Phase: phase.Load().(string)}
			s.IngestrCPU, s.IngestrRSSMB = soakIngestrProcess(bin)
			names := []string{pgContainer}
			if srContainer != "" {
				names = append(names, srContainer)
			}
			stats := soakDockerStats(names...)
			for name, v := range stats {
				switch {
				case strings.HasPrefix(pgContainer, name) || strings.HasPrefix(name, pgContainer[:12]):
					s.PGCPU, s.PGMemMB = v[0], v[1]
				case name == srContainer:
					s.SRCPU, s.SRMemMB = v[0], v[1]
				}
			}
			var commits, changes, wal, lag float64
			_ = srcPool.QueryRow(samplerCtx, `SELECT xact_commit::float8, (tup_inserted + tup_updated + tup_deleted)::float8 FROM pg_stat_database WHERE datname = current_database()`).Scan(&commits, &changes)
			_ = srcPool.QueryRow(samplerCtx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::float8, COALESCE((SELECT max(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)) FROM pg_replication_slots WHERE NOT temporary), 0)::float8`).Scan(&wal, &lag)
			s.SlotLagBytes = lag
			m := soakIngestrMetrics()
			s.rowsSyncedCounter = m["ingestr_stream_rows_synced_total"]
			s.SecondsBehind = m["ingestr_replication_seconds_behind"]
			tablesMu.Lock()
			names = names[:0]
			for _, tbl := range tables {
				names = append(names, tbl.name)
			}
			tablesMu.Unlock()
			for _, name := range names {
				var n int64
				if sr.QueryRowContext(samplerCtx, fmt.Sprintf("SELECT count(*) FROM %s.`%s` WHERE _cdc_deleted", srStressDatabase, name)).Scan(&n) == nil {
					s.LiveTombstones += n
				}
			}
			if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
				fields := strings.Fields(strings.Trim(string(out), "{} \n"))
				if len(fields) > 0 {
					s.HostLoad1, _ = strconv.ParseFloat(fields[0], 64)
				}
			}
			if prev != nil {
				dt := s.At.Sub(prev.At).Seconds()
				s.PGCommitsPerSec = (commits - prevCommits) / dt
				s.PGRowChangesPerS = (changes - prevChanges) / dt
				s.WALBytesPerSec = (wal - prevWAL) / dt
				if s.rowsSyncedCounter >= prev.rowsSyncedCounter {
					s.RowsSyncedPerSec = (s.rowsSyncedCounter - prev.rowsSyncedCounter) / dt
				}
			}
			prevCommits, prevChanges, prevWAL = commits, changes, wal
			resourcesMu.Lock()
			resources = append(resources, s)
			resourcesMu.Unlock()
			prev = &s
		}
	}()

	// --- initial snapshot ---
	expectedSeed := map[string]int64{}
	for _, tbl := range tables {
		expectedSeed[tbl.name] = tbl.nextID.Load()
		if tbl.simple != nil {
			expectedSeed[tbl.name] = int64(soakEvolvingRows)
		}
	}
	for {
		select {
		case <-sinkDone:
			t.Fatalf("ingestr stopped during the initial snapshot: %v", sink.fatalErr())
		default:
		}
		done := true
		for name, want := range expectedSeed {
			var n int64
			if sr.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s.`%s`", srStressDatabase, name)).Scan(&n) != nil || n != want {
				done = false
				break
			}
		}
		var probeExists int
		_ = sr.QueryRow("SELECT count(*) FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", srStressDatabase, soakProbeTable).Scan(&probeExists)
		if done && probeExists == 1 {
			break
		}
		if time.Since(snapshotStart) > 60*time.Minute {
			t.Fatal("initial snapshot did not complete within 60m")
		}
		time.Sleep(2 * time.Second)
	}
	snapshotTook := time.Since(snapshotStart)
	t.Logf("initial snapshot: %d rows visible in StarRocks after %v (%.0f rows/s)", seededRows, snapshotTook.Round(time.Second), float64(seededRows)/snapshotTook.Seconds())

	// --- load ---
	loadDuration := soakSteady + soakChaos
	phase.Store("steady")
	loadCtx, stopLoad := context.WithTimeout(ctx, loadDuration)
	defer stopLoad()
	ops := &soakOps{counts: map[string]int64{}, rows: map[string]int64{}}
	gen := newCDCStressLoadGen(t, soakWorkers, soakOpsPerSec, loadDuration)
	pickTable := func(rng *rand.Rand) *soakTable {
		tablesMu.Lock()
		defer tablesMu.Unlock()
		return tables[rng.Intn(len(tables))]
	}
	richTables := slices.Clone(tables[:soakTables])
	var wg sync.WaitGroup
	gen.start(loadCtx, &wg, func(rng *rand.Rand) (string, int64, error) {
		tbl := pickTable(rng)
		var kind, genKind string
		var affected int64
		var err error
		if tbl.rich {
			kind, genKind, affected, err = soakRichOp(ctx, srcPool, rng, tbl)
		} else {
			kind, genKind, affected, err = soakSimpleOp(ctx, srcPool, rng, tbl.simple)
		}
		if err != nil {
			return genKind, 0, fmt.Errorf("%s on %s: %w", kind, tbl.name, err)
		}
		ops.add(kind, affected)
		return genKind, max(affected, 1), nil
	})

	// Bulk statements: range deletes and range updates of thousands of rows in
	// one transaction.
	wg.Add(1)
	go func() {
		defer wg.Done()
		rng := rand.New(rand.NewSource(42))
		for {
			select {
			case <-loadCtx.Done():
				return
			case <-time.After(soakBulkEvery):
			}
			tbl := richTables[rng.Intn(len(richTables))]
			start := rng.Int63n(tbl.nextID.Load()-10_000) + 1
			size := int64(1000 + rng.Intn(4000))
			tag, err := srcPool.Exec(ctx, fmt.Sprintf("DELETE FROM public.%s WHERE id BETWEEN $1 AND $2", tbl.name), start, start+size)
			if err != nil {
				gen.recordOpErr(fmt.Errorf("bulk delete: %w", err))
				return
			}
			ops.add("bulk-delete (rows)", tag.RowsAffected())
			start = rng.Int63n(tbl.nextID.Load()-10_000) + 1
			tag, err = srcPool.Exec(ctx, fmt.Sprintf("UPDATE public.%s SET val = val + 7, status = 'bulk', updated_at = now() WHERE id BETWEEN $1 AND $2", tbl.name), start, start+5000)
			if err != nil {
				gen.recordOpErr(fmt.Errorf("bulk update: %w", err))
				return
			}
			ops.add("bulk-update (rows)", tag.RowsAffected())
		}
	}()

	probes := &soakProbes{pending: map[int64]*soakProbeWait{}, soft: !soakHardDelete}
	pollStart := time.Now()
	pollCtx, stopPoll := context.WithCancel(ctx)
	pollDone := make(chan struct{})
	go func() { defer close(pollDone); probes.poll(pollCtx, sr) }()
	var probeWG sync.WaitGroup
	probeWG.Add(1)
	go func() { defer probeWG.Done(); probes.launch(loadCtx, srcPool, &phase, gen.recordOpErr) }()

	// Chaos phase: kills, schema changes, a late table, and a targeted kill
	// while StarRocks converts a column.
	var ddlDone atomic.Int64
	ddlPhases := srStressSchemaPhases()
	type alterObservation struct {
		JobID, Table, State, Created, Finished string
		KilledIngestr                          bool
		DMLDuringJob                           int
	}
	var alters []alterObservation
	var altersMu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		if soakChaos == 0 {
			return
		}
		select {
		case <-loadCtx.Done():
			return
		case <-time.After(soakSteady):
		}
		phase.Store("chaos")
		sink.chaos.Store(true)
		go func() {
			<-loadCtx.Done()
			sink.chaos.Store(false)
		}()
		t.Logf("entering chaos phase: kills every ~%v, %d schema phases, 1 late table", srStressKillEvery, len(ddlPhases))
		// Watch for StarRocks schema-change jobs and kill ingestr once while one runs.
		go func() {
			seen := map[string]bool{}
			killed := false
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-loadCtx.Done():
					return
				case <-ticker.C:
				}
				rows, err := sr.Query("SHOW ALTER TABLE COLUMN FROM " + srStressDatabase)
				if err != nil {
					continue
				}
				cols, _ := rows.Columns()
				for rows.Next() {
					vals := make([]sql.NullString, len(cols))
					ptrs := make([]any, len(cols))
					for i := range vals {
						ptrs[i] = &vals[i]
					}
					if rows.Scan(ptrs...) != nil {
						continue
					}
					get := func(name string) string {
						for i, c := range cols {
							if strings.EqualFold(c, name) {
								return vals[i].String
							}
						}
						return ""
					}
					id := get("JobId")
					if seen[id] || get("State") != "RUNNING" {
						continue
					}
					seen[id] = true
					obs := alterObservation{JobID: id, Table: get("TableName"), State: "RUNNING", Created: get("CreateTime")}
					if !killed {
						killed = true
						targetedKill.Store(true)
						obs.KilledIngestr = exec.Command("pkill", "-9", "-f", bin+" ingest").Run() == nil
						t.Logf("SIGKILLed ingestr while StarRocks schema-change job %s on %s is RUNNING", id, obs.Table)
					}
					altersMu.Lock()
					alters = append(alters, obs)
					altersMu.Unlock()
				}
				_ = rows.Close()
			}
		}()
		// Leave the last 40% of the chaos window for the pipeline to absorb the
		// schema changes under continued kills.
		ddlDelay := soakChaos * 6 / 10 / time.Duration(len(ddlPhases))
		for i, ddl := range ddlPhases {
			select {
			case <-loadCtx.Done():
				gen.recordOpErr(fmt.Errorf("chaos window ended before schema phase %q", ddl.name))
				return
			case <-time.After(ddlDelay):
			}
			if _, err := srcPool.Exec(ctx, ddl.sql); err != nil {
				gen.recordOpErr(fmt.Errorf("schema phase %q: %w", ddl.name, err))
				return
			}
			ddlDone.Add(1)
			t.Logf("schema phase %d/%d: %s", i+1, len(ddlPhases), ddl.name)
			if i == 1 {
				name := stressTableName(90)
				if err := stressCreateAndSeed(ctx, srcPool, name, soakLateRows); err != nil {
					gen.recordOpErr(fmt.Errorf("late table: %w", err))
					return
				}
				tablesMu.Lock()
				tables = append(tables, &soakTable{name: name, simple: newStressTable(name, int64(soakLateRows))})
				tablesMu.Unlock()
				t.Logf("created late table %s with %d rows", name, soakLateRows)
			}
		}
	}()

	loadDone := make(chan struct{})
	go func() { wg.Wait(); probeWG.Wait(); close(loadDone) }()
	status := time.NewTicker(30 * time.Second)
	defer status.Stop()
	loadStart := time.Now()
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case <-sinkDone:
			stopLoad()
			<-loadDone
			t.Fatalf("ingestr supervisor stopped during load: %v", sink.fatalErr())
		case <-status.C:
			resourcesMu.Lock()
			last := soakResourceSample{}
			if len(resources) > 0 {
				last = resources[len(resources)-1]
			}
			resourcesMu.Unlock()
			probes.mu.Lock()
			nSamples := len(probes.samples)
			probes.mu.Unlock()
			t.Logf("t=%v [%s] %s | probes=%d slot lag=%.1fMB synced=%.0f rows/s tombstones=%d | sink %s",
				time.Since(loadStart).Round(time.Second), phase.Load(), gen.status(), nSamples, last.SlotLagBytes/1e6, last.RowsSyncedPerSec, last.LiveTombstones, sink.summary())
		}
	}
	loadTook := time.Since(loadStart)
	if soakChaos > 0 {
		require.Equal(t, int64(len(ddlPhases)), ddlDone.Load(), "all schema phases must complete")
	}
	achieved := gen.verify()
	sink.chaos.Store(false)
	phase.Store("converge")

	// --- convergence ---
	var finalLSN string
	require.NoError(t, srcPool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&finalLSN))
	convergeStart := time.Now()
	for {
		select {
		case <-sinkDone:
			t.Fatalf("ingestr supervisor stopped during convergence: %v", sink.fatalErr())
		default:
		}
		var caughtUp int
		require.NoError(t, srcPool.QueryRow(ctx, `SELECT count(*) FROM pg_replication_slots WHERE NOT temporary AND confirmed_flush_lsn >= $1::pg_lsn`, finalLSN).Scan(&caughtUp))
		if caughtUp >= 1 && probes.outstanding() == 0 {
			break
		}
		if time.Since(convergeStart) > srStressConverge {
			stressDumpReplicationState(t, ctx, srcPool)
			t.Fatalf("did not converge within %v (outstanding probes=%d)", srStressConverge, probes.outstanding())
		}
		time.Sleep(time.Second)
	}
	convergeTook := time.Since(convergeStart)
	t.Logf("converged %v after load stopped", convergeTook.Round(time.Second))
	stopPoll()
	<-pollDone
	pollPeriod := time.Since(pollStart) / time.Duration(max(probes.polls.Load(), 1))
	time.Sleep(2 * soakSampleEvery)
	stopSampler()
	<-samplerDone
	stopSink()
	<-sinkDone
	require.NoError(t, sink.fatalErr())
	require.NoError(t, sink.scanLogs(), "ingestr logs contain a panic or runtime crash")

	// --- verification ---
	verifyStart := time.Now()
	var mismatches []string
	var mismatchMu sync.Mutex
	var verifyWG sync.WaitGroup
	var liveRows, totalTombstones atomic.Int64
	verifyTables := append([]string{soakProbeTable}, func() []string {
		names := make([]string, 0, len(tables))
		for _, tbl := range tables {
			names = append(names, tbl.name)
		}
		return names
	}()...)
	for _, name := range verifyTables {
		verifyWG.Add(1)
		go func() {
			defer verifyWG.Done()
			fail := func(format string, args ...any) {
				mismatchMu.Lock()
				mismatches = append(mismatches, fmt.Sprintf(name+": "+format, args...))
				mismatchMu.Unlock()
			}
			dest := srStressDatabase + "." + quoteStarRocksStressIdent(name)
			var tombstones, nulls, total int64
			if err := sr.QueryRow("SELECT COALESCE(SUM(CASE WHEN _cdc_deleted THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN _cdc_deleted IS NULL OR _cdc_lsn IS NULL THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN _cdc_deleted THEN 0 ELSE 1 END), 0) FROM "+dest).Scan(&tombstones, &nulls, &total); err != nil {
				fail("count query: %v", err)
				return
			}
			if soakHardDelete && tombstones != 0 {
				fail("%d tombstones remain with hard deletes", tombstones)
			}
			if !soakHardDelete {
				totalTombstones.Add(tombstones)
			}
			if nulls != 0 {
				fail("%d rows have NULL _cdc_deleted or _cdc_lsn", nulls)
			}
			var srcCount int64
			if err := verifyPool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM public.%s", quoteStressIdentifier(name))).Scan(&srcCount); err != nil {
				fail("source count: %v", err)
				return
			}
			if srcCount != total {
				fail("row count: source=%d StarRocks=%d", srcCount, total)
			}
			if err := srStressCompareRows(ctx, verifyPool, sr, name, dest); err != nil {
				fail("%v", err)
				return
			}
			liveRows.Add(total)
			t.Logf("%s: %d live rows match value by value, %d tombstones", name, total, tombstones)
		}()
	}
	verifyWG.Wait()
	verifyTook := time.Since(verifyStart)
	require.Empty(t, mismatches, "StarRocks diverged from the source:\n%s", strings.Join(mismatches, "\n"))

	// DML StarRocks accepted while a schema-change job was converting a table.
	altersMu.Lock()
	for i, a := range alters {
		var created, finished, state string
		rows, err := sr.Query("SHOW ALTER TABLE COLUMN FROM " + srStressDatabase)
		if err == nil {
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]sql.NullString, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				_ = rows.Scan(ptrs...)
				m := map[string]string{}
				for i, c := range cols {
					m[c] = vals[i].String
				}
				if m["JobId"] == a.JobID {
					created, finished, state = m["CreateTime"], m["FinishTime"], m["State"]
				}
			}
			_ = rows.Close()
		}
		alters[i].Created, alters[i].Finished, alters[i].State = created, finished, state
		var n int
		_ = sr.QueryRow("SELECT count(*) FROM information_schema.loads WHERE DB_NAME = ? AND TABLE_NAME = ? AND CREATE_TIME >= ? AND CREATE_TIME <= ?", srStressDatabase, a.Table, created, finished).Scan(&n)
		alters[i].DMLDuringJob = n
	}
	altersMu.Unlock()

	// --- report ---
	var flushDurations, flushRows []float64
	for _, path := range sink.logs {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for scanner.Scan() {
			if m := soakFlushLine.FindStringSubmatch(scanner.Text()); m != nil {
				rows, _ := strconv.ParseFloat(m[1], 64)
				d, _ := time.ParseDuration(m[2])
				flushRows = append(flushRows, rows)
				flushDurations = append(flushDurations, d.Seconds())
			}
		}
		_ = f.Close()
	}
	var srLoads, srDeletes int
	_ = sr.QueryRow("SELECT count(*), SUM(CASE WHEN LABEL LIKE 'delete_%' THEN 1 ELSE 0 END) FROM information_schema.loads WHERE DB_NAME = ?", srStressDatabase).Scan(&srLoads, &srDeletes)

	probes.mu.Lock()
	samples := slices.Clone(probes.samples)
	probes.mu.Unlock()
	resourcesMu.Lock()
	res := slices.Clone(resources)
	resourcesMu.Unlock()

	var report strings.Builder
	host, _ := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	cores, _ := exec.Command("sysctl", "-n", "hw.ncpu").Output()
	mem, _ := exec.Command("sysctl", "-n", "hw.memsize").Output()
	memBytes, _ := strconv.ParseFloat(strings.TrimSpace(string(mem)), 64)
	dockerInfo, _ := exec.Command("docker", "info", "--format", "{{.OperatingSystem}}, {{.NCPU}} CPUs, {{.MemTotal}} bytes").Output()
	var srVersion string
	_ = sr.QueryRow("SELECT current_version()").Scan(&srVersion)
	mode := map[bool]string{true: "hard", false: "soft"}[soakHardDelete]
	fmt.Fprintf(&report, "# PostgreSQL CDC → StarRocks soak, --cdc-delete-behavior=%s\n\n", mode)
	fmt.Fprintf(&report, "Host: %s, %s cores, %.0f GiB RAM. Containers: %s (PostgreSQL 16, StarRocks %s). ingestr runs natively on the host.\n\n",
		strings.TrimSpace(string(host)), strings.TrimSpace(string(cores)), memBytes/(1<<30), strings.TrimSpace(string(dockerInfo)), srVersion)
	fmt.Fprintf(&report, "Config: %d rich tables × %d rows + evolving table %d rows (+ late table %d rows), %d load workers at %d ops/s target, %.0f probe keys/s (insert, update, update, delete each), flush interval %v / %d records, steady %v + chaos %v (kill every ~%v).\n\n",
		soakTables, soakSeedRows, soakEvolvingRows, soakLateRows, soakWorkers, soakOpsPerSec, soakProbeRate, srStressFlushInterval, soakFlushRecords, soakSteady, soakChaos, srStressKillEvery)
	fmt.Fprintf(&report, "## Throughput\n\n")
	fmt.Fprintf(&report, "- Initial snapshot: %d rows in %v (%.0f rows/s)\n", seededRows, snapshotTook.Round(time.Second), float64(seededRows)/snapshotTook.Seconds())
	fmt.Fprintf(&report, "- Load: %v, %.0f effective source mutations/s achieved (target %d)\n", loadTook.Round(time.Second), achieved, soakOpsPerSec)
	fmt.Fprintf(&report, "- Convergence after load stopped: %v; verification of %d rows: %v\n", convergeTook.Round(time.Second), liveRows.Load(), verifyTook.Round(time.Second))
	fmt.Fprintf(&report, "- ingestr: %s; flushes parsed: %d; StarRocks loads in `stress`: %d (%d deletes)\n\n", sink.summary(), len(flushDurations), srLoads, srDeletes)
	fmt.Fprintf(&report, "Source operations committed:\n\n| operation | transactions | rows |\n|---|---:|---:|\n")
	ops.mu.Lock()
	kinds := make([]string, 0, len(ops.counts))
	for k := range ops.counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(&report, "| %s | %d | %d |\n", k, ops.counts[k], ops.rows[k])
	}
	ops.mu.Unlock()
	fmt.Fprintf(&report, "\n## End-to-end latency (source commit → visible in StarRocks)\n\n")
	fmt.Fprintf(&report, "Latency is measured from the source COMMIT returning to the first StarRocks poll that sees the change; polls ran back to back every %v on average (the measurement resolution). ", pollPeriod.Round(time.Millisecond))
	if soakHardDelete {
		fmt.Fprintf(&report, "`delete (tombstone or gone)` is when readers filtering `NOT _cdc_deleted` stop seeing the row; `delete (row gone)` is when it is physically removed. A tombstone was caught by a poll for %d of %d deletes; `delete tombstone exposure` lists how long those were visible.\n\n", probes.tombstoneSeen.Load(), probes.deletes.Load())
	} else {
		report.WriteString("In soft mode a delete is visible once its tombstone is.\n\n")
	}
	report.WriteString(soakLatencyTable(samples, []string{"steady", "chaos", "all"}))
	report.WriteString("\n### Distributions\n\n```\n")
	for _, ph := range []string{"steady", "chaos"} {
		for _, op := range []string{"insert", "update", "delete (tombstone or gone)", "delete (row gone)", "delete tombstone exposure"} {
			report.WriteString(soakHistogram(samples, ph, op))
		}
	}
	report.WriteString("```\n\n## Pipeline and resources\n\n")
	fmt.Fprintf(&report, "- Flush cycle duration (s): %s\n- Rows per flush: %s\n", soakSummary(flushDurations), soakSummary(flushRows))
	for _, ph := range []string{"steady", "chaos"} {
		var icpu, irss, pgcpu, pgmem, srcpu, srmem, commits, changes, walMB, lagMB, synced, tomb, load []float64
		for _, s := range res {
			if s.Phase != ph {
				continue
			}
			icpu, irss = append(icpu, s.IngestrCPU), append(irss, s.IngestrRSSMB)
			pgcpu, pgmem, srcpu, srmem = append(pgcpu, s.PGCPU), append(pgmem, s.PGMemMB), append(srcpu, s.SRCPU), append(srmem, s.SRMemMB)
			commits, changes = append(commits, s.PGCommitsPerSec), append(changes, s.PGRowChangesPerS)
			walMB, lagMB = append(walMB, s.WALBytesPerSec/1e6), append(lagMB, s.SlotLagBytes/1e6)
			synced = append(synced, s.RowsSyncedPerSec)
			tomb, load = append(tomb, float64(s.LiveTombstones)), append(load, s.HostLoad1)
		}
		fmt.Fprintf(&report, "\n**%s phase** (%d samples every %v)\n\n| metric | summary |\n|---|---|\n", ph, len(icpu), soakSampleEvery)
		for _, row := range []struct {
			name string
			v    []float64
		}{
			{"ingestr CPU % (of one core)", icpu},
			{"ingestr RSS MB", irss},
			{"PostgreSQL CPU %", pgcpu},
			{"PostgreSQL mem MB", pgmem},
			{"StarRocks CPU %", srcpu},
			{"StarRocks mem MB", srmem},
			{"source commits/s", commits},
			{"source row changes/s", changes},
			{"WAL MB/s", walMB},
			{"slot lag MB", lagMB},
			{"rows synced/s (ingestr metric)", synced},
			{"tombstones visible in StarRocks", tomb},
			{"host load avg (1m)", load},
		} {
			fmt.Fprintf(&report, "| %s | %s |\n", row.name, soakSummary(row.v))
		}
	}
	report.WriteString("\n## StarRocks schema-change jobs during the chaos phase\n\n")
	for _, a := range alters {
		fmt.Fprintf(&report, "- job %s on %s: %s, created %s, finished %s, ingestr SIGKILLed while RUNNING: %v, loads committed into the table during the job: %d\n",
			a.JobID, a.Table, a.State, a.Created, a.Finished, a.KilledIngestr, a.DMLDuringJob)
	}
	if len(alters) == 0 {
		report.WriteString("- none observed (conversions finished between polls or used fast schema evolution)\n")
	}
	fmt.Fprintf(&report, "\n## Verification\n\n- %d tables compared value by value against the source: all match, %d tombstones, 0 NULL CDC columns.\n", len(verifyTables), totalTombstones.Load())

	require.NoError(t, os.WriteFile(filepath.Join(resultsDir, "report.md"), []byte(report.String()), 0o644))
	sampleJSON, _ := json.Marshal(samples)
	require.NoError(t, os.WriteFile(filepath.Join(resultsDir, "latency_samples.json"), sampleJSON, 0o644))
	resJSON, _ := json.Marshal(res)
	require.NoError(t, os.WriteFile(filepath.Join(resultsDir, "resources.json"), resJSON, 0o644))
	t.Log("\n" + report.String())
}
