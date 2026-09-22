package jira

// The Teams client against a server that behaves the way Atlassian's
// does, including the part that surprises everybody: the member listing
// is a POST, and a GET to it is 405.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Placeholders, and they have to stay placeholders. A real organisation
// or team id identifies the organisation, and a test fixture is a file in
// a public repository like any other.
const (
	testOrgID  = "00000000-0000-0000-0000-000000000000"
	testTeamID = "11111111-1111-1111-1111-111111111111"
)

// teamsServer returns a client pointed at h, which is the only way these
// tests reach a "team" at all.
func teamsServer(t *testing.T, h http.HandlerFunc) *TeamsClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := NewTeams(testOrgID, testTeamID, "someone@example.com", "token", 5)
	if err != nil {
		t.Fatalf("NewTeams: %v", err)
	}
	c.host = srv.URL
	return c
}

func TestTeamsClientReadsTheTeam(t *testing.T) {
	var gotMethod, gotAuth string
	c := teamsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		if r.URL.Path != "/public/teams/v1/org/"+testOrgID+"/teams/"+testTeamID {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"displayName":"Platform","state":"ACTIVE","description":"x"}`)
	})

	team, err := c.Team(context.Background())
	if err != nil {
		t.Fatalf("Team: %v", err)
	}
	if team.DisplayName != "Platform" {
		t.Errorf("displayName = %q, want Platform", team.DisplayName)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	// Basic, not Bearer: the whole reason this needs no OAuth is that it
	// takes the same credentials as the rest of the tool.
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("Authorization = %q, want Basic", gotAuth)
	}
}

// The members endpoint is a POST with the page size in the body. Sending
// a GET is what a reasonable person writes first, and it answers 405.
func TestMemberIDsPostsWithAPageSize(t *testing.T) {
	var body map[string]any
	c := teamsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, `{"results":[{"accountId":"acc-a"},{"accountId":"acc-b"}],
			"pageInfo":{"hasNextPage":false,"endCursor":""}}`)
	})

	ids, err := c.MemberIDs(context.Background())
	if err != nil {
		t.Fatalf("MemberIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "acc-a" || ids[1] != "acc-b" {
		t.Errorf("ids = %v", ids)
	}
	if body["first"] == nil {
		t.Errorf("body = %v, want a page size", body)
	}
	if _, asked := body["after"]; asked {
		t.Errorf("the first page asked for a cursor: %v", body)
	}
}

// A team larger than one page must not come back truncated, which is the
// failure mode nobody notices until somebody is missing from a report.
func TestMemberIDsFollowsPages(t *testing.T) {
	page := 0
	c := teamsServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		page++
		if page == 1 {
			_, _ = io.WriteString(w, `{"results":[{"accountId":"acc-a"}],
				"pageInfo":{"hasNextPage":true,"endCursor":"next"}}`)
			return
		}
		if body["after"] != "next" {
			t.Errorf("second page asked with after = %v, want the first page's cursor", body["after"])
		}
		_, _ = io.WriteString(w, `{"results":[{"accountId":"acc-b"}],
			"pageInfo":{"hasNextPage":false}}`)
	})

	ids, err := c.MemberIDs(context.Background())
	if err != nil {
		t.Fatalf("MemberIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want both pages", ids)
	}
}

// A cursor that never says it is finished must not loop forever.
func TestMemberIDsStopsOnAnEndlessCursor(t *testing.T) {
	calls := 0
	c := teamsServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"results":[{"accountId":"acc-a"}],
			"pageInfo":{"hasNextPage":true,"endCursor":"same"}}`)
	})

	if _, err := c.MemberIDs(context.Background()); err != nil {
		t.Fatalf("MemberIDs: %v", err)
	}
	if calls != maxMemberPages {
		t.Errorf("made %d calls, want it to give up after %d", calls, maxMemberPages)
	}
}

// An error is the one string that gets copied into a bug report, so it
// must not carry the ids that identify the organisation.
func TestTeamsErrorsDoNotQuoteTheIDs(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		c := teamsServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			// Atlassian echoes the ids back in its own error bodies, which
			// is exactly why none of the body is repeated.
			_, _ = io.WriteString(w, `{"error":"no team `+testTeamID+` in org `+testOrgID+`"}`)
		})
		_, err := c.Team(context.Background())
		if err == nil {
			t.Fatalf("%d: want an error", status)
		}
		if strings.Contains(err.Error(), testOrgID) || strings.Contains(err.Error(), testTeamID) {
			t.Errorf("%d: error quotes an id: %v", status, err)
		}
	}
}

// Both halves or neither: one id without the other is a mistake, not half
// a configuration, and the import should never be offered for it.
func TestNewTeamsNeedsBothIDs(t *testing.T) {
	cases := [][2]string{{"", ""}, {testOrgID, ""}, {"", testTeamID}, {testOrgID, "../other"}}
	for _, c := range cases {
		if _, err := NewTeams(c[0], c[1], "someone@example.com", "token", 5); err == nil {
			t.Errorf("NewTeams(%q, %q) was accepted", c[0], c[1])
		}
	}
}

// The client is read-only by the same rule as the Jira one: an allowlist
// of paths, because the verb cannot decide.
func TestTeamsClientRefusesAWrite(t *testing.T) {
	if err := assertTeamsRead(http.MethodDelete, "/public/teams/v1/org/x/teams/y"); err == nil {
		t.Error("a DELETE was allowed")
	}
	if err := assertTeamsRead(http.MethodPost, "/public/teams/v1/org/x/teams/y"); err == nil {
		t.Error("a POST to something other than the member listing was allowed")
	}
	if err := assertTeamsRead(http.MethodPost, "/public/teams/v1/org/x/teams/y/members"); err != nil {
		t.Errorf("the member listing was refused: %v", err)
	}
}
