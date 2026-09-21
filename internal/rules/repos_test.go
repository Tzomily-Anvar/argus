package rules

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// recorder answers every request with the same repository list and keeps
// what was asked for, which is the part under test: the three listings
// are not interchangeable and picking the wrong one is either a 404 or a
// quietly incomplete answer.
type recorder struct {
	mu    sync.Mutex
	paths []string
	query []url.Values
	body  string
	srv   *httptest.Server
}

func newRecorder(t *testing.T, body string) *recorder {
	t.Helper()
	r := &recorder{body: body}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.paths = append(r.paths, req.URL.Path)
		r.query = append(r.query, req.URL.Query())
		r.mu.Unlock()
		fmt.Fprint(w, r.body)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *recorder) asked() ([]string, []url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paths, r.query
}

func testContext(srvURL string, kind gh.AccountKind, account, me string) *Context {
	return &Context{
		Client:      gh.NewForTest(srvURL, "test-credential"),
		Org:         account,
		Account:     kind,
		Me:          me,
		Concurrency: 2,
		Scope:       Scope{Org: account, Personal: kind.Personal()},
	}
}

const twoRepos = `[{"name":"api","archived":false},{"name":"web","archived":false}]`

// Which endpoint a repository listing goes to, and with which
// parameters. /orgs/{org}/repos answers 404 for a person, and
// /user/repos rejects type and affiliation together with a 422, so both
// halves of each case matter.
func TestRepoListingEndpointPerAccountKind(t *testing.T) {
	cases := []struct {
		name      string
		kind      gh.AccountKind
		account   string
		me        string
		wantPath  string
		wantQuery map[string]string
		bannedKey string
	}{
		{
			name:      "an organisation is listed as before",
			kind:      gh.AccountOrganisation,
			account:   "your-org",
			me:        "octocat",
			wantPath:  "/orgs/your-org/repos",
			wantQuery: map[string]string{"type": "all", "sort": "pushed"},
		},
		{
			name:    "your own account uses the listing that carries private repositories",
			kind:    gh.AccountUser,
			account: "octocat",
			me:      "octocat",
			// /users/octocat/repos would be public repositories only,
			// which is a silently short answer rather than an error.
			wantPath:  "/user/repos",
			wantQuery: map[string]string{"affiliation": "owner", "sort": "pushed"},
			bannedKey: "type",
		},
		{
			// GitHub treats logins as case-insensitive, and the
			// configured name is typed by hand.
			name:      "capitalisation does not lose you your own private repositories",
			kind:      gh.AccountUser,
			account:   "OctoCat",
			me:        "octocat",
			wantPath:  "/user/repos",
			wantQuery: map[string]string{"affiliation": "owner"},
			bannedKey: "type",
		},
		{
			name:      "somebody else's account is read by name",
			kind:      gh.AccountUser,
			account:   "example",
			me:        "octocat",
			wantPath:  "/users/example/repos",
			wantQuery: map[string]string{"type": "owner", "sort": "pushed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, twoRepos)
			c := testContext(rec.srv.URL, tc.kind, tc.account, tc.me)

			repos, err := c.Repos()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repos) != 2 {
				t.Fatalf("got %d repositories, want 2", len(repos))
			}

			paths, query := rec.asked()
			if len(paths) != 1 || paths[0] != tc.wantPath {
				t.Fatalf("asked %v, want a single %s", paths, tc.wantPath)
			}
			for k, want := range tc.wantQuery {
				if got := query[0].Get(k); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
			if tc.bannedKey != "" && query[0].Get(tc.bannedKey) != "" {
				t.Errorf("%s was sent as well; GitHub answers 422 when it is paired with affiliation",
					tc.bannedKey)
			}
		})
	}
}

// The scope rules apply to every kind of account, and they apply in the
// listing rather than in each rule that uses it.
func TestRepoListingAppliesScope(t *testing.T) {
	const mixed = `[{"name":"api","archived":false},{"name":"old","archived":true},{"name":"sandbox","archived":false}]`

	cases := []struct {
		name  string
		scope Scope
		want  []string
	}{
		{"archived left out by default", Scope{Org: "your-org"}, []string{"api", "sandbox"}},
		{"archived on request", Scope{Org: "your-org", Archived: true}, []string{"api", "old", "sandbox"}},
		{"an exclusion", Scope{Org: "your-org", Excluded: []string{"sandbox"}}, []string{"api"}},
		{"an allowlist", Scope{Org: "your-org", Only: []string{"api"}}, []string{"api"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, mixed)
			c := testContext(rec.srv.URL, gh.AccountOrganisation, "your-org", "octocat")
			c.Scope = tc.scope

			repos, err := c.Repos()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got []string
			for _, r := range repos {
				got = append(got, gh.Str(r["name"]))
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("repositories = %v, want %v", got, tc.want)
			}
		})
	}
}

// Two rules want this list and they run at the same time. Listing an
// account's repositories twice per sweep is a cost nobody asked for.
func TestRepoListingIsSharedAcrossTheSweep(t *testing.T) {
	rec := newRecorder(t, twoRepos)
	c := testContext(rec.srv.URL, gh.AccountOrganisation, "your-org", "octocat")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Repos(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if paths, _ := rec.asked(); len(paths) != 1 {
		t.Errorf("listed repositories %d times, want 1", len(paths))
	}
}
