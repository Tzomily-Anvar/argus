package rules

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

func TestApprovalState(t *testing.T) {
	cases := []struct {
		github              string
		approvals, required int
		want                string
	}{
		// Stacked on a branch the rulesets miss: GitHub says approved on one.
		{"APPROVED", 1, 2, "REVIEW_REQUIRED"},
		{"APPROVED", 2, 2, "APPROVED"},
		{"APPROVED", 3, 2, "APPROVED"},
		// Nothing asked for, or nothing readable: GitHub's word stands.
		{"APPROVED", 1, 0, "APPROVED"},
		// A change requested is never overruled.
		{"CHANGES_REQUESTED", 2, 2, "CHANGES_REQUESTED"},
		{"REVIEW_REQUIRED", 1, 2, "REVIEW_REQUIRED"},
		{"", 0, 2, "NONE"},
	}
	for _, c := range cases {
		if got := approvalState(c.github, c.approvals, c.required); got != c.want {
			t.Errorf("approvalState(%q, %d, %d) = %q, want %q", c.github, c.approvals, c.required, got, c.want)
		}
	}
}

func TestRequiredApprovalsTakesTheLargestAndAsksOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/repos/your-org/web-app/rules/branches/main":
			// A repository ruleset and the organisation's, and a rule that
			// is not about pull requests at all.
			w.Write([]byte(`[
				{"type":"pull_request","ruleset_source_type":"Repository","parameters":{"required_approving_review_count":1}},
				{"type":"pull_request","ruleset_source_type":"Organization","parameters":{"required_approving_review_count":2}},
				{"type":"deletion"}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer srv.Close()
	c := &Context{Org: "your-org", Client: gh.NewForTest(srv.URL, "ghp_x")}

	if got := c.RequiredApprovals("web-app", "main"); got != 2 {
		t.Errorf("got %d, want the larger of the two rulesets, 2", got)
	}
	c.RequiredApprovals("web-app", "main")
	if n := calls.Load(); n != 1 {
		t.Errorf("asked %d times, want once per sweep", n)
	}
	if got := c.RequiredApprovals("unreadable", "main"); got != 0 {
		t.Errorf("unreadable rules gave %d, want 0 so GitHub's decision stands", got)
	}
}

func TestMyReview(t *testing.T) {
	review := func(state, oid string) any {
		return map[string]any{"state": state, "commit": map[string]any{"oid": oid}}
	}
	pr := func(reviews ...any) map[string]any {
		return map[string]any{"mine": map[string]any{"nodes": reviews}}
	}
	cases := []struct {
		name               string
		pr                 map[string]any
		wantState, wantOid string
	}{
		{"never reviewed", pr(), "", ""},
		{"only comments", pr(review("COMMENTED", "a"), review("COMMENTED", "b")), "COMMENTED", "b"},
		// A comment after an approval keeps the approval but moves the commit.
		{"approved then commented", pr(review("APPROVED", "a"), review("COMMENTED", "b")), "APPROVED", "b"},
		{"changes then approved", pr(review("CHANGES_REQUESTED", "a"), review("APPROVED", "b")), "APPROVED", "b"},
		// A review not yet submitted is not read.
		{"pending ignored", pr(review("COMMENTED", "a"), review("PENDING", "b")), "COMMENTED", "a"},
		{"dismissed counts as a look", pr(review("DISMISSED", "a")), "COMMENTED", "a"},
	}
	for _, c := range cases {
		state, oid := myReview(c.pr)
		if state != c.wantState || oid != c.wantOid {
			t.Errorf("%s: myReview = %q on %q, want %q on %q", c.name, state, oid, c.wantState, c.wantOid)
		}
	}
}
