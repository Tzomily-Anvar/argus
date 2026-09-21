package rules

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Security alerts on a personal account.
//
// GitHub publishes the alert feeds account-wide for an organisation
// only, so the same rows have to be collected a repository at a time.
// These hold that path to producing the same shape, and to failing
// loudly when it produces nothing - an empty alert list reads as "you
// are clean", and that is the wrong thing to believe by accident.

func alertServer(t *testing.T, handle func(repo string) (int, string)) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/repos") {
			fmt.Fprint(w, `[{"name":"api","archived":false},{"name":"web","archived":false}]`)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		status, body := handle(parts[2])
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := append([]string{}, asked...)
		sort.Strings(out)
		return out
	}
}

func TestPersonalAlertsAreCollectedPerRepository(t *testing.T) {
	srv, asked := alertServer(t, func(repo string) (int, string) {
		return http.StatusOK, fmt.Sprintf(
			`[{"number":1,"html_url":"https://github.com/octocat/%s/security/dependabot/1"}]`, repo)
	})
	c := testContext(srv.URL, gh.AccountUser, "octocat", "octocat")

	alerts, err := accountAlerts(c, "dependabot/alerts", Params("state", "open"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want one from each repository", len(alerts))
	}

	// The per-repository feed leaves out which repository the alert
	// belongs to. Everything downstream reads it, so it has to be put
	// back or every alert lands under an empty repository name.
	var repos []string
	for _, a := range gh.Maps(alerts) {
		repos = append(repos, gh.Str(gh.Map(a["repository"])["name"]))
	}
	sort.Strings(repos)
	if fmt.Sprint(repos) != "[api web]" {
		t.Errorf("alerts came back attributed to %v, want [api web]", repos)
	}

	want := "[/repos/octocat/api/dependabot/alerts /repos/octocat/web/dependabot/alerts /user/repos]"
	if got := fmt.Sprint(asked()); got != want {
		t.Errorf("asked %s, want %s", got, want)
	}
}

// An organisation still goes to the one endpoint it has always used.
func TestOrganisationAlertsUseTheAccountWideFeed(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		fmt.Fprint(w, `[{"number":1,"repository":{"name":"api"}}]`)
	}))
	defer srv.Close()

	c := testContext(srv.URL, gh.AccountOrganisation, "your-org", "octocat")
	if _, err := accountAlerts(c, "dependabot/alerts", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"/orgs/your-org/dependabot/alerts"}
	if fmt.Sprint(asked) != fmt.Sprint(want) {
		t.Errorf("asked %v, want %v", asked, want)
	}
}

// A repository with the feature switched off is ordinary and is skipped.
// Every repository refusing is not, and has to be said rather than
// returned as an empty list.
func TestPersonalAlerts(t *testing.T) {
	cases := []struct {
		name      string
		answer    func(repo string) (int, string)
		wantCount int
		errHas    string
	}{
		{
			name: "one repository with the feature off is skipped",
			answer: func(repo string) (int, string) {
				if repo == "api" {
					return http.StatusForbidden, `{"message":"Dependabot alerts are disabled"}`
				}
				return http.StatusOK, `[{"number":1}]`
			},
			wantCount: 1,
		},
		{
			name: "every repository refusing is an error, not a clean bill of health",
			answer: func(string) (int, string) {
				return http.StatusForbidden, `{"message":"Dependabot alerts are disabled"}`
			},
			errHas: "read per repository",
		},
		{
			name: "nothing found anywhere is genuinely nothing",
			answer: func(string) (int, string) {
				return http.StatusOK, `[]`
			},
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := alertServer(t, tc.answer)
			c := testContext(srv.URL, gh.AccountUser, "octocat", "octocat")

			alerts, err := accountAlerts(c, "dependabot/alerts", nil)
			switch {
			case tc.errHas != "":
				if err == nil {
					t.Fatalf("got %d alerts, want an error mentioning %q", len(alerts), tc.errHas)
				}
				if !strings.Contains(err.Error(), tc.errHas) {
					t.Errorf("error = %v, want it to mention %q", err, tc.errHas)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			case len(alerts) != tc.wantCount:
				t.Errorf("got %d alerts, want %d", len(alerts), tc.wantCount)
			}
		})
	}
}
