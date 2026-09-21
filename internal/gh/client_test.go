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
