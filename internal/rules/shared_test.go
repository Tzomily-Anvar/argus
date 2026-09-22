package rules

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// rateLimitFixture is a rate limit as the client would report one.
func rateLimitFixture() error { return &gh.RateLimitError{Resource: gh.BucketSearch} }

// Filtering one shared listing has to give each rule exactly the set its
// own search gave it. The author qualifier is where that is easiest to
// get wrong: search wants app/dependabot and the result it returns is
// authored by dependabot[bot].
func TestMatchesAuthorSpeaksBothSpellings(t *testing.T) {
	bot := map[string]any{"user": map[string]any{"login": "dependabot[bot]"}}
	person := map[string]any{"user": map[string]any{"login": "octocat"}}

	cases := []struct {
		item      map[string]any
		qualifier string
		want      bool
	}{
		{bot, "app/dependabot", true},
		{bot, "dependabot", false},
		{person, "octocat", true},
		{person, "OctoCat", true}, // GitHub logins are case-insensitive
		{person, "app/dependabot", false},
		{bot, "app/renovate", false},
	}
	for _, c := range cases {
		if got := MatchesAuthor(c.item, c.qualifier); got != c.want {
			t.Errorf("MatchesAuthor(%q, %q) = %v, want %v",
				AuthorLogin(c.item), c.qualifier, got, c.want)
		}
	}
}

// The shared listing is only a valid stand-in while it is complete.
// Search stops at a thousand results however many match, so past that
// each rule has to pay for its own narrower query rather than report a
// truncated answer as a whole one.
func TestOpenPRsWhereFallsBackWhenTheListingIsTruncated(t *testing.T) {
	c := &Context{}
	c.openPRsOnce.Do(func() {
		c.openPRs = []map[string]any{{"number": 1.0}}
		c.openPRsFull = false
	})

	_, complete, err := c.OpenPRs()
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if complete {
		t.Fatal("a truncated listing must not report itself complete")
	}

	// The fallback goes to search, so the item it returns is the server's
	// rather than the one in the listing it was told not to trust.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"items":[{"number":99}]}`))
	}))
	defer srv.Close()
	c.Client = gh.NewForTest(srv.URL, "ghp_x")

	got, err := c.OpenPRsWhere("is:pr is:open", func(map[string]any) bool { return false })
	if err != nil {
		t.Fatalf("OpenPRsWhere: %v", err)
	}
	if len(got) != 1 || got[0]["number"] != 99.0 {
		t.Errorf("a truncated listing must send the caller to its own search, got %v", got)
	}
}

func TestOpenPRsWhereFiltersTheSharedListing(t *testing.T) {
	c := &Context{}
	c.openPRsOnce.Do(func() {
		c.openPRs = []map[string]any{
			{"number": 1.0, "draft": true},
			{"number": 2.0, "draft": false},
		}
		c.openPRsFull = true
	})

	got, err := c.OpenPRsWhere("unused", func(it map[string]any) bool {
		return it["draft"] != true
	})
	if err != nil {
		t.Fatalf("OpenPRsWhere: %v", err)
	}
	if len(got) != 1 || got[0]["number"] != 2.0 {
		t.Errorf("filter returned %v", got)
	}
}

// Batching several repositories into one document is the cheapest thing
// available to the branch rule, but it must still be a read. The
// fragment therefore follows the query rather than opening the document.
func TestBatchedBranchQueryIsStillReadOnly(t *testing.T) {
	query, vars, aliases := branchQueryFor([]string{"example", "other-repo"})

	if !strings.HasPrefix(strings.TrimSpace(query), "query") {
		t.Fatalf("the read-only guard requires a document beginning with query:\n%s", query)
	}
	if len(aliases) != 2 || aliases[0] != "r0" || aliases[1] != "r1" {
		t.Errorf("aliases = %v", aliases)
	}
	if vars["n0"] != "example" || vars["n1"] != "other-repo" {
		t.Errorf("repository names must travel as variables, not interpolated: %v", vars)
	}
	if strings.Contains(query, "other-repo") {
		t.Error("a repository name was interpolated into the document")
	}
	if strings.Count(query, "...branches") != 2 || !strings.Contains(query, "fragment branches") {
		t.Errorf("each repository should reuse one fragment:\n%s", query)
	}
}

// A rate limit is not a shorter answer, it is a wrong one.
func TestUnpackFailsTheRuleOnARateLimit(t *testing.T) {
	ok := []checked{{row: map[string]any{"repo": "example"}}, {row: nil}}
	rows, err := unpack(ok)
	if err != nil || len(rows) != 2 {
		t.Fatalf("ordinary results must pass through: %v %v", rows, err)
	}

	limited := append(ok, checked{err: rateLimitFixture()})
	if _, err := unpack(limited); err == nil {
		t.Error("a rate limit among the results must fail the whole rule")
	}
}

func TestFirstRateLimitIgnoresOrdinaryFailures(t *testing.T) {
	ordinary := []error{http.ErrNoLocation, nil}
	if err := firstRateLimit(ordinary); err != nil {
		t.Errorf("an ordinary failure is not a rate limit: %v", err)
	}
	if err := firstRateLimit(append(ordinary, rateLimitFixture())); err == nil {
		t.Error("a rate limit among ordinary failures must be found")
	}
}
