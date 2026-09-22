package rules_test

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
	"github.com/Tzomily-Anvar/argus/internal/rules"
)

// What a sweep actually costs, measured rather than guessed.
//
// This exists because the cost cannot be measured from outside the
// process. GET /rate_limit does not itself count against the limit, and
// on a fine-grained token it has been observed reporting a bucket the
// sweep is demonstrably spending as untouched - so the only trustworthy
// place to count is the client that makes the requests.
//
// Skipped unless a credential is present, so `go test ./...` stays
// offline everywhere:
//
//	ARGUS_GITHUB_TOKEN=... ARGUS_GITHUB_ORG=your-org \
//	  go test ./internal/rules/ -run LiveAPIBudget -v -timeout 20m
//
// It runs each rule on its own, which is not how a sweep runs them, and
// that is the point: concurrently the totals are identical but nothing
// can be attributed. The concurrent pass at the end is there for wall
// time, which is the number sequential running does misrepresent.
func TestLiveAPIBudget(t *testing.T) {
	if _, err := config.Load(); err != nil {
		t.Fatalf("loading configuration: %v", err)
	}
	if config.ArgusToken() == "" && config.AmbientToken() == "" {
		t.Skip("set ARGUS_GITHUB_TOKEN to run the budget measurement")
	}

	client, err := gh.NewFromEnv()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	start := client.Budget()
	t0 := time.Now()
	ctx, err := rules.NewContext(client)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	report(t, "(context)", client.Budget().Sub(start), time.Since(t0))

	enabled := rules.Enabled()
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].ID < enabled[j].ID })

	for _, r := range enabled {
		before := client.Budget()
		ruleStart := time.Now()
		_, err := r.Run(ctx, rules.Resolve(r))
		elapsed := time.Since(ruleStart)
		spent := client.Budget().Sub(before)
		if err != nil {
			t.Logf("%-18s FAILED: %v", r.ID, err)
		}
		report(t, r.ID, spent, elapsed)
		if points, ok := spent.GraphQLPointsSince(before); ok && spent.GraphQL > 0 {
			t.Logf("%-18s graphql points: %d for %d calls", r.ID, points, spent.GraphQL)
		}
	}

	total := client.Budget().Sub(start)
	report(t, "TOTAL sequential", total, time.Since(t0))
	for _, l := range sorted(client.Budget().Limits) {
		t.Logf("  bucket %-8s used %d of %d, %d left, resets %s",
			l.Resource, l.Used, l.Limit, l.Remaining, l.Reset.Format(time.TimeOnly))
	}

	// The same work the way the sweep does it, for wall time. A fresh
	// context, because the shared repository listing is already warm.
	if os.Getenv("ARGUS_BUDGET_CONCURRENT") == "" {
		return
	}
	before := client.Budget()
	t1 := time.Now()
	ctx2, err := rules.NewContext(client)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	gh.PMap(enabled, len(enabled), func(r *rules.Rule) int {
		if _, err := r.Run(ctx2, rules.Resolve(r)); err != nil {
			t.Logf("%s: %v", r.ID, err)
		}
		return 0
	})
	report(t, "TOTAL concurrent", client.Budget().Sub(before), time.Since(t1))
}

func report(t *testing.T, label string, b gh.Budget, d time.Duration) {
	t.Helper()
	t.Logf("%-18s rest=%-4d search=%-3d graphql=%-4d pages=%-3d free=%-3d retries=%-2d total=%-4d %s",
		label, b.REST, b.Search, b.GraphQL, b.Pages, b.NotModified, b.Retries, b.Total(),
		d.Round(time.Millisecond))
}

func sorted(limits map[string]gh.RateLimit) []gh.RateLimit {
	out := make([]gh.RateLimit, 0, len(limits))
	for _, l := range limits {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}
