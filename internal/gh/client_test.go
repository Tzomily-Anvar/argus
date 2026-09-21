package gh

import (
	"net/http"
	"strings"
	"testing"
)

// The read-only guarantee is the main safety claim Argus makes, so it is
// tested rather than merely documented. assertReadOnly is the single
// chokepoint every request passes through.

func TestRejectsWriteMethods(t *testing.T) {
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions,
	} {
		err := assertReadOnly(method, apiBase+"/repos/o/r/issues/1/comments", nil)
		if err == nil {
			t.Errorf("%s against the REST API was allowed; it must be refused", method)
			continue
		}
		if _, ok := err.(*WriteAttemptError); !ok {
			t.Errorf("%s: got %T, want *WriteAttemptError", method, err)
		}
	}
}

func TestAllowsGet(t *testing.T) {
	if err := assertReadOnly(http.MethodGet, apiBase+"/user", nil); err != nil {
		t.Fatalf("GET must be allowed, got %v", err)
	}
}

func TestGraphQLQueriesAllowed(t *testing.T) {
	queries := []string{
		`query($a:String!){ repository(owner:$a){ name } }`,
		`{ viewer { login } }`,
		"\n\n  query Foo { viewer { login } }",
		"# a leading comment\nquery { viewer { login } }",
	}
	for _, q := range queries {
		body := map[string]any{"query": q}
		if err := assertReadOnly(http.MethodPost, graphqlURL, body); err != nil {
			t.Errorf("query %q must be allowed, got %v", truncate(q), err)
		}
	}
}

func TestGraphQLMutationsRefused(t *testing.T) {
	mutations := []string{
		`mutation { addComment(input:{}) { clientMutationId } }`,
		"  \n mutation Foo { deleteRef(input:{}) { clientMutationId } }",
		"# sneaky\nmutation { mergePullRequest(input:{}) { clientMutationId } }",
		"",
	}
	for _, m := range mutations {
		body := map[string]any{"query": m}
		err := assertReadOnly(http.MethodPost, graphqlURL, body)
		if err == nil {
			t.Errorf("mutation %q was allowed; it must be refused", truncate(m))
			continue
		}
		if _, ok := err.(*WriteAttemptError); !ok {
			t.Errorf("mutation %q: got %T, want *WriteAttemptError", truncate(m), err)
		}
	}
}

// A POST is tolerated only for GraphQL. Pointing one anywhere else, even
// with a well-formed query body, must still be refused.
func TestPostOnlyToGraphQLEndpoint(t *testing.T) {
	body := map[string]any{"query": "query { viewer { login } }"}
	if err := assertReadOnly(http.MethodPost, apiBase+"/repos/o/r/merges", body); err == nil {
		t.Fatal("POST to a REST endpoint was allowed; it must be refused")
	}
}

func TestStripLeadingComments(t *testing.T) {
	cases := map[string]string{
		"# one\n# two\nquery { a }": "query { a }",
		"   query { a }":            "query { a }",
		"# only a comment":          "",
	}
	for in, want := range cases {
		if got := strings.TrimSpace(stripLeadingComments(in)); got != want {
			t.Errorf("stripLeadingComments(%q) = %q, want %q", in, got, want)
		}
	}
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

// The token's prefix identifies its kind exactly, which is what lets the
// error messages point at the right screen.
func TestDetectTokenType(t *testing.T) {
	cases := map[string]TokenType{
		"github_pat_EXAMPLE": TokenFineGrained,
		"ghp_EXAMPLE":        TokenClassic,
		"gho_EXAMPLE":        TokenOAuth,
		"ghu_EXAMPLE":        TokenAppUser,
		"ghs_EXAMPLE":        TokenAppInstall,
		"1234567890abcdef":   TokenUnknown,
		"":                   TokenUnknown,
	}
	// Deliberately not token-shaped beyond the prefix: a fixture long
	// enough to look like a credential trips the repository's own
	// private-string check, and only the prefix is under test.
	for token, want := range cases {
		if got := DetectTokenType(token); got != want {
			t.Errorf("DetectTokenType(%.14s…) = %q, want %q", token, got, want)
		}
	}
}

// The advice has to differ, because "check the scopes" sends someone
// holding a fine-grained token to a screen that does not exist.
func TestAdviceDiffersByTokenType(t *testing.T) {
	fine := TokenFineGrained.permissionAdvice()
	classic := TokenClassic.permissionAdvice()
	if fine == classic {
		t.Fatal("fine-grained and classic tokens need different advice")
	}
	if !strings.Contains(classic, "scopes") {
		t.Error("classic advice should mention scopes")
	}
	if !strings.Contains(fine, "approved") {
		t.Error("fine-grained advice should mention organisation approval, the most common trap")
	}
	if strings.Contains(fine, "scopes") {
		t.Error("fine-grained advice must not send people looking for scopes")
	}
}

// GraphQL answers partially. A token that cannot read one field gets an
// error for that field and real data for the rest, and throwing the lot
// away turns "check status unavailable" into "nothing is open".
func TestGraphQLPartialDataIsKept(t *testing.T) {
	// data present alongside errors: partial, and the data must survive.
	payload := map[string]any{
		"data":   map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewDecision": "APPROVED"}}},
		"errors": []any{map[string]any{"type": "FORBIDDEN", "message": "Resource not accessible by personal access token"}},
	}
	data, err := decodeGraphQL(payload)
	if !IsPartial(err) {
		t.Fatalf("expected a partial error, got %v", err)
	}
	if data == nil {
		t.Fatal("partial data must be returned, not discarded")
	}
	repo, _ := data["repository"].(map[string]any)
	pr, _ := repo["pullRequest"].(map[string]any)
	if pr["reviewDecision"] != "APPROVED" {
		t.Errorf("the field that did resolve was lost: %v", data)
	}
}

func TestGraphQLErrorsWithoutDataStayFatal(t *testing.T) {
	payload := map[string]any{
		"errors": []any{map[string]any{"message": "Could not resolve to a Repository"}},
	}
	data, err := decodeGraphQL(payload)
	if err == nil {
		t.Fatal("errors with no data must be an error")
	}
	if IsPartial(err) {
		t.Error("nothing resolved, so this is a failure rather than a partial answer")
	}
	if data != nil {
		t.Error("no data should be returned")
	}
}
