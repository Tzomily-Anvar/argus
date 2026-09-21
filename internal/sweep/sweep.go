// Package sweep keeps a warm, in-memory picture of the organisation.
//
// The reason this exists is latency. Most rules answer in a few seconds,
// but the org-wide security sweep depends on an endpoint that paginates
// through an opaque cursor and has been observed taking well over a
// minute. If the dashboard swept on page load, every visit would cost
// that wait.
//
// So it does not. A goroutine sweeps on a timer in the background and
// keeps the latest result for each rule in memory. Opening the dashboard
// reads what is already there and renders immediately, labelled with how
// old it is. The slow work still happens - it just never happens while
// someone is waiting for it.
//
// Nothing is persisted. The cache lives as long as the process, which is
// the whole point of leaving the container running.
package sweep

import (
	"log"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/gh"
	"github.com/Tzomily-Anvar/argus/internal/rules"
)

// Result is one rule's most recent outcome.
type Result struct {
	RuleID     string `json:"rule_id"`
	Data       any    `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
	FinishedAt string `json:"finished_at"`
	DurationMS int64  `json:"duration_ms"`
}

// Snapshot is everything the dashboard needs for one render.
type Snapshot struct {
	Org        string            `json:"org"`
	User       string            `json:"user"`
	Teams      []string          `json:"teams"`
	Results    map[string]Result `json:"results"`
	SweptAt    string            `json:"swept_at"`
	AgeSeconds int               `json:"age_seconds"`
	Sweeping   bool              `json:"sweeping"`
	Error      string            `json:"error,omitempty"`
	Ready      bool              `json:"ready"`
}

// Cache holds the latest snapshot and runs the background refresh.
type Cache struct {
	client   *gh.Client
	interval time.Duration

	mu       sync.RWMutex
	results  map[string]Result
	org      string
	user     string
	teams    []string
	sweptAt  time.Time
	sweeping bool
	lastErr  string

	trigger chan struct{}
}

// New returns a cache that refreshes every `interval`.
func New(client *gh.Client, interval time.Duration) *Cache {
	if interval < time.Minute {
		interval = time.Minute
	}
	return &Cache{
		client:   client,
		interval: interval,
		results:  map[string]Result{},
		// Buffered so a refresh request never blocks the HTTP handler,
		// and depth 1 so several clicks coalesce into one sweep rather
		// than queueing up duplicates of expensive work.
		trigger: make(chan struct{}, 1),
	}
}

// Start begins sweeping: once immediately, then on the interval, plus
// whenever Refresh is called. Returns straight away.
func (c *Cache) Start() {
	go func() {
		c.Sweep()
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.Sweep()
			case <-c.trigger:
				c.Sweep()
			}
		}
	}()
}

// Refresh asks for a sweep now. Non-blocking, and a no-op if one is
// already queued.
func (c *Cache) Refresh() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

// Sweep runs every enabled rule concurrently and replaces the snapshot.
// A rule that fails records its error and leaves the others untouched.
func (c *Cache) Sweep() {
	c.mu.Lock()
	if c.sweeping {
		c.mu.Unlock()
		return
	}
	c.sweeping = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.sweeping = false
		c.mu.Unlock()
	}()

	started := time.Now()
	ctx, err := rules.NewContext(c.client)
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.sweptAt = time.Now().UTC()
		c.mu.Unlock()
		log.Printf("sweep: could not start: %v", err)
		return
	}

	enabled := rules.Enabled()
	out := gh.PMap(enabled, len(enabled), func(r *rules.Rule) Result {
		ruleStart := time.Now()
		data, err := r.Run(ctx, rules.Resolve(r))
		res := Result{
			RuleID:     r.ID,
			FinishedAt: time.Now().UTC().Format(time.RFC3339),
			DurationMS: time.Since(ruleStart).Milliseconds(),
		}
		if err != nil {
			res.Error = err.Error()
			log.Printf("sweep: rule %s failed: %v", r.ID, err)
		} else {
			res.Data = data
		}
		return res
	})

	results := make(map[string]Result, len(out))
	for _, r := range out {
		results[r.RuleID] = r
	}

	c.mu.Lock()
	c.results = results
	c.org, c.user, c.teams = ctx.Org, ctx.Me, ctx.Teams
	c.sweptAt = time.Now().UTC()
	c.lastErr = ""
	c.mu.Unlock()

	log.Printf("sweep: %d rules in %s", len(enabled), time.Since(started).Round(time.Millisecond))
}

// Snapshot returns the current picture. Safe to call at any time; before
// the first sweep completes it reports Ready false.
func (c *Cache) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	results := make(map[string]Result, len(c.results))
	for k, v := range c.results {
		results[k] = v
	}
	teams := c.teams
	if teams == nil {
		teams = []string{}
	}

	snap := Snapshot{
		Org:      c.org,
		User:     c.user,
		Teams:    teams,
		Results:  results,
		Sweeping: c.sweeping,
		Error:    c.lastErr,
		Ready:    !c.sweptAt.IsZero() && len(c.results) > 0,
	}
	if !c.sweptAt.IsZero() {
		snap.SweptAt = c.sweptAt.Format(time.RFC3339)
		snap.AgeSeconds = int(time.Since(c.sweptAt).Seconds())
	}
	return snap
}
