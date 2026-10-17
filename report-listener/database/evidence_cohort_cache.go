package database

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cleanapp-common/evidencesourceprojection"
)

// Opt-in process-local cache of guard-free brand cohorts (design C″; see
// docs/brand-cohort-cache.md). A cohort is one repeatable-read snapshot of a
// brand scope's membership, identity, chosen analysis, evidence bits, source
// tuple and case references. Every publication guard is evaluated per request
// from live blocking sets read inside the request's own transaction, so counts,
// grouping, samples and privacy are live by construction; only membership,
// public identity and the source tuple are as old as the snapshot. Refreshes
// keep bounded held keys warm, are single-flight per key, bounded in time, and
// cancelled on Stop(). The direct path is untouched while no cohort
// is held, its snapshot has expired, or the cache is disabled (default).
type evidenceCohortCacheConfig struct {
	enabled          bool
	interval, budget time.Duration
	maxKeys          int
	warm             []string
}

// Defaults: refresh snapshots after 60 s, 60 s cohort read budget, at most 8
// cohorts (ESTIMATE ~40 MB each for a 61k-report brand). Intervals shorter than
// a second would make the failure backoff degenerate, so they are floored.
func evidenceCohortCacheConfigFromEnv() evidenceCohortCacheConfig {
	cfg := evidenceCohortCacheConfig{
		enabled:  os.Getenv("EVIDENCE_BRAND_COHORT_CACHE") == "1",
		interval: time.Duration(envInt([]string{"EVIDENCE_BRAND_COHORT_REFRESH_SEC"}, 60)) * time.Second,
		budget:   time.Duration(envInt([]string{"EVIDENCE_BRAND_COHORT_BUDGET_SEC"}, 60)) * time.Second,
		maxKeys:  envInt([]string{"EVIDENCE_BRAND_COHORT_MAX_KEYS"}, 8),
	}
	for _, name := range strings.Split(os.Getenv("EVIDENCE_BRAND_COHORT_WARM"), ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			cfg.warm = append(cfg.warm, name)
		}
	}
	return cfg.normalized()
}

func (cfg evidenceCohortCacheConfig) normalized() evidenceCohortCacheConfig {
	cfg.interval = max(cfg.interval, time.Second)
	cfg.budget = max(cfg.budget, time.Second)
	cfg.maxKeys = max(cfg.maxKeys, 1)
	return cfg
}

type evidenceCohortEntry struct {
	key         string
	filter      EvidenceFilter
	cohort      *evidenceCohort
	refreshedAt time.Time
	refreshing  bool
	failures    int
	retryAfter  time.Time
}

type evidenceCohortCache struct {
	once       sync.Once
	configured bool
	config     evidenceCohortCacheConfig
	load       func(ctx context.Context, f EvidenceFilter, budget time.Duration) (evidenceCohort, error)
	now        func() time.Time
	mu         sync.Mutex
	entries    map[string]*list.Element
	recent     *list.List // front = most recently requested
	inflight   sync.WaitGroup
	base       context.Context
	cancel     context.CancelFunc
	loadGate   chan struct{}
	stopped    bool
	// Tests can supply controlled ticks; production owns one cache-wide ticker.
	maintenanceTicks    <-chan time.Time
	maintenanceDone     chan struct{}
	maintenancePassDone chan<- struct{} // optional controlled-scheduler acknowledgment
}

func (d *Database) evidenceCohortCache() *evidenceCohortCache {
	c := &d.evidenceCohorts
	c.once.Do(func() {
		if !c.configured {
			c.config = evidenceCohortCacheConfigFromEnv()
		}
		c.config = c.config.normalized()
		c.entries = map[string]*list.Element{}
		c.recent = list.New()
		c.base, c.cancel = context.WithCancel(context.Background())
		c.loadGate = make(chan struct{}, 1)
		if c.load == nil {
			c.load = d.loadEvidenceCohortSnapshot
		}
		if c.now == nil {
			c.now = time.Now
		}
		if c.config.enabled {
			c.maintenanceDone = make(chan struct{})
			go c.maintain()
		}
	})
	return c
}

// configure fixes the configuration before first use instead of reading the
// environment. Fixtures use it; production always reads the environment.
func (c *evidenceCohortCache) configure(cfg evidenceCohortCacheConfig) {
	c.config, c.configured = cfg, true
}

// The cohort depends only on the brand scope and the requested analysis
// language; kind, sort, limit, offset and the overview clock are request-local.
func evidenceCohortKey(f EvidenceFilter) string {
	brands := append([]string(nil), f.Brands...)
	sort.Strings(brands)
	key, _ := json.Marshal(append([]string{f.Language}, brands...))
	return string(key)
}

// lookup returns the held cohort for the filter's scope and records the demand.
// An absent or stale entry schedules one background refresh unless one is in
// flight, backing off or the cache is stopped; the caller falls through to the
// direct path when nothing usable is held. A snapshot can be served for at
// most interval+budget from its first read, including failed-refresh backoff.
// Expired snapshots remain retained for retry bookkeeping, never for serving.
// Keys are bounded: a new key evicts the
// least recently requested idle cohort, and is not admitted while every held
// cohort is refreshing.
func (c *evidenceCohortCache) lookup(f EvidenceFilter) (evidenceCohort, bool) {
	if !c.config.enabled {
		return evidenceCohort{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.admit(f)
	if entry == nil {
		return evidenceCohort{}, false
	}
	now := c.now()
	c.scheduleDue(entry, now)
	if entry.cohort == nil || entry.cohort.snapshotAt.IsZero() || now.Sub(entry.cohort.snapshotAt) >= c.config.interval+c.config.budget {
		return evidenceCohort{}, false
	}
	return *entry.cohort, true
}

// scheduleDue is shared by requests and idle upkeep. Callers hold c.mu, so
// Stop's stopped flag and every WaitGroup Add are ordered under the same lock.
func (c *evidenceCohortCache) scheduleDue(entry *evidenceCohortEntry, now time.Time) {
	if c.stopped || entry.refreshing || now.Before(entry.retryAfter) {
		return
	}
	start := entry.refreshedAt
	if entry.cohort != nil && !entry.cohort.snapshotAt.IsZero() {
		start = entry.cohort.snapshotAt
	}
	if entry.cohort != nil && now.Sub(start) < c.config.interval {
		return
	}
	entry.refreshing = true
	c.inflight.Add(1)
	go c.run(entry)
}

// One maintenance loop keeps only currently admitted keys warm. It neither
// admits new keys nor changes request recency, and evicted keys own no timers.
func (c *evidenceCohortCache) maintain() {
	defer close(c.maintenanceDone)
	ticks := c.maintenanceTicks
	if ticks == nil {
		ticker := time.NewTicker(min(time.Second, c.config.interval/4))
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		select {
		case <-c.base.Done():
			return
		case _, open := <-ticks:
			if !open {
				return
			}
			c.mu.Lock()
			now := c.now()
			for element := c.recent.Back(); element != nil; element = element.Prev() {
				c.scheduleDue(element.Value.(*evidenceCohortEntry), now)
			}
			c.mu.Unlock()
			if c.maintenancePassDone != nil {
				select {
				case c.maintenancePassDone <- struct{}{}:
				case <-c.base.Done():
					return
				}
			}
		}
	}
}

// admit returns the entry for the filter, creating it within the key bound.
// Callers hold c.mu.
func (c *evidenceCohortCache) admit(f EvidenceFilter) *evidenceCohortEntry {
	key := evidenceCohortKey(f)
	if element, ok := c.entries[key]; ok {
		c.recent.MoveToFront(element)
		return element.Value.(*evidenceCohortEntry)
	}
	for len(c.entries) >= c.config.maxKeys {
		var victim *list.Element
		for element := c.recent.Back(); element != nil; element = element.Prev() {
			if !element.Value.(*evidenceCohortEntry).refreshing {
				victim = element
				break
			}
		}
		if victim == nil {
			return nil
		}
		c.recent.Remove(victim)
		delete(c.entries, victim.Value.(*evidenceCohortEntry).key)
	}
	entry := &evidenceCohortEntry{key: key, filter: f}
	c.entries[key] = c.recent.PushFront(entry)
	return entry
}

func (c *evidenceCohortCache) run(entry *evidenceCohortEntry) {
	defer c.inflight.Done()
	// Full-cohort reads share the primary database. Queue requested keys behind
	// one cancellable slot; each read gets its own budget only after admission.
	select {
	case c.loadGate <- struct{}{}:
		defer func() { <-c.loadGate }()
	case <-c.base.Done():
		c.mu.Lock()
		entry.refreshing = false
		c.mu.Unlock()
		return
	}
	if c.base.Err() != nil {
		c.mu.Lock()
		entry.refreshing = false
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(c.base, c.config.budget)
	defer cancel()
	_ = c.refresh(ctx, entry)
}

func (c *evidenceCohortCache) refresh(ctx context.Context, entry *evidenceCohortEntry) error {
	cohort, err := c.load(ctx, entry.filter, c.config.budget)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.refreshing = false
	now := c.now()
	if err != nil {
		// A failed refresh keeps the previous cohort and backs off. SQL and
		// driver errors can contain source values or transport details, so only
		// the failure class is logged.
		entry.failures++
		backoff := c.config.interval << uint(min(entry.failures, 4))
		entry.retryAfter = now.Add(backoff)
		log.Printf("Evidence brand cohort refresh failed for %q (%s); previous cohort retained, retry in %s", entry.filter.BrandName, evidenceCohortFailureClass(err), backoff)
		return err
	}
	entry.failures, entry.retryAfter = 0, time.Time{}
	entry.cohort, entry.refreshedAt = &cohort, now
	return nil
}

func evidenceCohortFailureClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "budget exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "query error"
	}
}

// refreshNow loads the filter's cohort synchronously, honouring single flight
// and the key bound.
func (c *evidenceCohortCache) refreshNow(ctx context.Context, f EvidenceFilter) error {
	c.mu.Lock()
	entry := c.admit(f)
	if entry == nil {
		c.mu.Unlock()
		return fmt.Errorf("evidence cohort cache is full of refreshing cohorts")
	}
	if entry.refreshing {
		c.mu.Unlock()
		return fmt.Errorf("evidence cohort refresh already in flight")
	}
	entry.refreshing = true
	c.mu.Unlock()
	return c.refresh(ctx, entry)
}

// stop cancels in-flight refreshes, prevents new ones and waits for the
// goroutines to exit. Held cohorts stay readable for requests still in flight.
func (c *evidenceCohortCache) stop() {
	c.mu.Lock()
	c.stopped = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c.maintenanceDone != nil {
		<-c.maintenanceDone
	}
	c.inflight.Wait()
}

// StopEvidenceBrandCohorts cancels background cohort refreshes; it is a no-op
// unless the cache was used.
func (d *Database) StopEvidenceBrandCohorts() {
	d.evidenceCohortCache().stop()
}

// WarmEvidenceBrandCohorts schedules the configured brands' first cohorts at
// startup. It is a no-op unless the cohort cache is enabled.
func (d *Database) WarmEvidenceBrandCohorts() {
	c := d.evidenceCohortCache()
	for _, name := range c.config.warm {
		f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: name})
		if err != nil {
			log.Printf("Evidence brand cohort warm list skips %q: %v", name, err)
			continue
		}
		c.lookup(f)
	}
}

// A background refresh reads one complete guard-free cohort in its own
// read-only repeatable-read snapshot under the configured budget rather than
// the public request deadline. It never answers a request directly.
func (d *Database) loadEvidenceCohortSnapshot(ctx context.Context, f EvidenceFilter, budget time.Duration) (evidenceCohort, error) {
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return evidenceCohort{}, err
	}
	defer tx.Rollback()
	query, args := projectedEvidenceCohortQuery(f, budget)
	cohort, err := loadProjectedEvidenceCohort(ctx, tx, f, query, args)
	if err != nil {
		return evidenceCohort{}, err
	}
	if err := tx.Commit(); err != nil {
		return evidenceCohort{}, err
	}
	return cohort, nil
}

// publicationBlocklists are the live blocking sets of the four publication
// guards, read as whole (small) sets rather than per-report lookups. Their cost
// is proportional to the number of blocked reports in the database, not to the
// cohort size; production measured 0 private owners, ~1.5k non-public raw rows,
// ~1.6k inactive statuses and 0 restricted projections (2026-10-17).
type publicationBlocklists struct {
	inactiveStatus, nonPublicRaw, privateOwner, privacyRestricted, dirtySources map[int]struct{}
}

func (b publicationBlocklists) blocked(seq int) bool {
	_, inactive := b.inactiveStatus[seq]
	_, nonPublic := b.nonPublicRaw[seq]
	_, private := b.privateOwner[seq]
	return inactive || nonPublic || private
}

// publicationBlocklistQueries are the five set reads, in execution order. The
// publication sets complement the direct statement's guards, evaluated by MySQL
// under the same column collations: a non-NULL status other than 'active'
// (report_status), a non-NULL visibility other than 'public' (the complement of
// PublicVisibilityWhereSQL), a non-empty owner with is_public=FALSE
// (reports_owners), and a projection with privacy_restricted=TRUE. Dirty source
// generations are restricted conservatively until the worker recomputes them:
// invalidation must never leave an old public privacy flag authoritative. NULL seqs
// never match a correlated guard, so they are excluded. The index hints name the
// covering indexes of the production schema; MySQL warns and scans when one is
// absent, without changing the answer.
func publicationBlocklistQueries() [5]string {
	budget := "MAX_EXECUTION_TIME(" + strconv.FormatInt(int64(projectedEvidenceRequestBudget/time.Millisecond), 10) + ")"
	return [5]string{
		"SELECT /*+ " + budget + " INDEX(rs idx_rs_seq_status) */ rs.seq FROM report_status rs WHERE rs.seq IS NOT NULL AND rs.status IS NOT NULL AND rs.status<>'active'",
		"SELECT /*+ " + budget + " INDEX(rr idx_visibility) */ rr.report_seq FROM report_raw rr WHERE rr.visibility IS NOT NULL AND rr.visibility<>'public'",
		"SELECT /*+ " + budget + " INDEX(ro idx_is_public) */ ro.seq FROM reports_owners ro WHERE ro.is_public=FALSE AND ro.seq IS NOT NULL AND ro.owner IS NOT NULL AND ro.owner<>''",
		"SELECT /*+ " + budget + " INDEX(p " + evidencesourceprojection.PrivacyRestrictedIndex + ") */ p.seq FROM " + evidencesourceprojection.Table + " p WHERE p.privacy_restricted=TRUE",
		"SELECT /*+ " + budget + " INDEX(p idx_dirty_seq) */ p.seq FROM " + evidencesourceprojection.Table + " p WHERE p.dirty=TRUE",
	}
}

// loadPublicationBlocklists reads the five live blocking sets inside the
// request's read-only repeatable-read transaction and deadline. Any failure
// fails the request closed; no partial set is ever applied.
func loadPublicationBlocklists(ctx context.Context, tx *sql.Tx) (publicationBlocklists, error) {
	queries := publicationBlocklistQueries()
	sets := [5]map[int]struct{}{}
	for i, query := range queries {
		set, err := loadEvidenceSeqSet(ctx, tx, query)
		if err != nil {
			return publicationBlocklists{}, fmt.Errorf("read publication blocklist: %w", err)
		}
		sets[i] = set
	}
	return publicationBlocklists{inactiveStatus: sets[0], nonPublicRaw: sets[1], privateOwner: sets[2], privacyRestricted: sets[3], dirtySources: sets[4]}, nil
}

func loadEvidenceSeqSet(ctx context.Context, tx *sql.Tx, query string) (map[int]struct{}, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[int]struct{}{}
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		set[seq] = struct{}{}
	}
	return set, rows.Err()
}

// applyPublicationBlocklists narrows a guard-free cohort to its live subset
// before any grouping: blocked reports are dropped, and PrivacyRestricted is
// taken from the live projection, with every dirty generation restricted before
// ranking, sample selection or hydration. A projection that was dirty or absent at
// refresh carried its exact fresh value into the cohort; its live column may
// still be the older generation, so a live restriction can add to that value
// but never remove it. The shared cohort is not mutated.
func applyPublicationBlocklists(cohort evidenceCohort, b publicationBlocklists) []evidenceRecord {
	live := make([]evidenceRecord, 0, len(cohort.records))
	for _, r := range cohort.records {
		if b.blocked(r.Seq) {
			continue
		}
		_, restricted := b.privacyRestricted[r.Seq]
		_, dirty := b.dirtySources[r.Seq]
		r.PrivacyRestricted = restricted || dirty || (cohort.freshSources[r.Seq] && r.PrivacyRestricted)
		live = append(live, r)
	}
	return live
}

// serveProjectedEvidenceCohort answers a request from a held cohort inside the
// request's own read-only transaction and deadline: live blocking sets, Go set
// difference, the existing pure grouping over the live subset, then the
// existing bounded hydration. The page carries the cohort's snapshot time.
func serveProjectedEvidenceCohort(ctx context.Context, tx *sql.Tx, f EvidenceFilter, cohort evidenceCohort) (EvidenceBrandPage, error) {
	blocklists, err := loadPublicationBlocklists(ctx, tx)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	live := applyPublicationBlocklists(cohort, blocklists)
	page, err := finishProjectedEvidencePage(ctx, tx, f, cohort, live, false)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	snapshotAt := cohort.snapshotAt
	page.SnapshotAt = &snapshotAt
	return page, nil
}
