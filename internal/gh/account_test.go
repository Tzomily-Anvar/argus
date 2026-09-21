package gh

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Telling an organisation from a person.
//
// This is the decision the whole personal-account path hangs off, and
// getting it wrong is silent: an organisation read as a person asks for
// endpoints that 404, and a person read as an organisation searches with
// org: and matches nothing at all. So it is tested against a server that
// answers the way GitHub does.

func TestAccountKindOf(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   AccountKind
		errHas string
	}{
		{
			name:   "an organisation",
			status: http.StatusOK,
			body:   `{"login":"your-org","type":"Organization"}`,
			want:   AccountOrganisation,
		},
		{
			name:   "a person",
			status: http.StatusOK,
			body:   `{"login":"octocat","type":"User"}`,
			want:   AccountUser,
		},
		{
			// Bots and mannequins have logins too, and neither has
			// repositories to sweep. Saying so beats an empty dashboard.
			name:   "something that is neither",
			status: http.StatusOK,
			body:   `{"login":"example","type":"Bot"}`,
			errHas: "neither an organisation nor a personal account",
		},
		{
			name:   "an answer with no type at all",
			status: http.StatusOK,
			body:   `{"login":"example"}`,
			errHas: "did not say what kind of account",
		},
		{
			name:   "a name GitHub does not know",
			status: http.StatusNotFound,
			body:   `{"message":"Not Found"}`,
			errHas: "404",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/users/") {
					t.Errorf("asked %s; the kind of an account is read from /users/{login}", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			got, err := NewForTest(srv.URL, "test-credential").AccountKindOf("example")
			switch {
			case tc.errHas != "":
				if err == nil {
					t.Fatalf("got kind %q, want an error mentioning %q", got, tc.errHas)
				}
				if !strings.Contains(err.Error(), tc.errHas) {
					t.Errorf("error = %v, want it to mention %q", err, tc.errHas)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			case got != tc.want:
				t.Errorf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}

// One sweep runs every rule, and each of them needs to know which kind of
// account this is. Asking GitHub once is the difference between a fact
// and a per-rule tax.
func TestAccountKindIsAskedOnce(t *testing.T) {
	var mu sync.Mutex
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		fmt.Fprint(w, `{"login":"octocat","type":"User"}`)
	}))
	defer srv.Close()

	c := NewForTest(srv.URL, "test-credential")
	for range 3 {
		if _, err := c.AccountKindOf("octocat"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// GitHub treats logins as case-insensitive, and the configured name
	// routinely differs in capitalisation from the one the API returns.
	if _, err := c.AccountKindOf("OctoCat"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if asked != 1 {
		t.Errorf("asked GitHub %d times, want 1", asked)
	}
}

// A failed lookup must not be remembered as an answer, or one flaky
// request would pin the whole process to the wrong endpoints.
func TestFailedLookupIsNotCached(t *testing.T) {
	var mu sync.Mutex
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		first := asked == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprint(w, `{"login":"your-org","type":"Organization"}`)
	}))
	defer srv.Close()

	c := NewForTest(srv.URL, "test-credential")
	if _, err := c.AccountKindOf("your-org"); err == nil {
		t.Fatal("a 404 must be an error")
	}
	kind, err := c.AccountKindOf("your-org")
	if err != nil {
		t.Fatalf("the second attempt should have gone out again: %v", err)
	}
	if kind != AccountOrganisation {
		t.Errorf("kind = %q, want %q", kind, AccountOrganisation)
	}
}
