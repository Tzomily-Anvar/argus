package jira

// The Atlassian Teams API, which is where a team's membership lives.
//
// It is a different host from the Jira site - api.atlassian.com rather
// than yoursite.atlassian.net - and a different API, but the same
// credentials: Basic with the Jira email and API token. That is the whole
// reason this is worth having. Importing a roster needs no OAuth dance,
// no second token and nothing further to create.
//
// Read-only, and for the same reason and by the same rule as the Jira
// client above it: the member listing is a POST because the query and its
// cursor travel in the body, so the verb cannot decide what is a read and
// an allowlist of paths does instead. Only two endpoints are ever built
// here, both of them privately, and both of them reads.
//
// One more rule, particular to this client: THE ORGANISATION AND TEAM IDS
// NEVER APPEAR IN AN ERROR. They identify the organisation, they are in
// every path this client builds, and an error message is the one string
// that gets copied into a bug report. So failures say which setting is
// wrong rather than quoting the URL that proved it.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// teamsHost is the Atlassian platform API. Held as a field on the client
// so a test can point it at an httptest server.
const teamsHost = "https://api.atlassian.com"

// membersPageSize is what one member request asks for. Fifty is the
// API's usual maximum and more than any team this tool reports on, so
// paging is the unusual path rather than the normal one.
const membersPageSize = 50

// maxMemberPages stops a broken cursor turning into an endless loop. At
// fifty a page this is two and a half thousand people, which is not a
// team.
const maxMemberPages = 50

// Team is an Atlassian team, reduced to what the roster needs. The
// organisation and team ids are deliberately not carried back out: the
// caller supplied them and nothing downstream should be handling them.
type Team struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	State       string `json:"state"`
}

// TeamsClient reads one team's membership. Safe for concurrent use.
type TeamsClient struct {
	http *http.Client
	host string
	auth string
	org  string
	team string
}

// NewTeams returns a client for one team, authenticating with the same
// email and API token as the Jira client.
func NewTeams(orgID, teamID, email, token string, timeoutSeconds int) (*TeamsClient, error) {
	orgID, teamID = strings.TrimSpace(orgID), strings.TrimSpace(teamID)
	if orgID == "" || teamID == "" {
		return nil, errf("no Atlassian team configured; set ARGUS_ATLASSIAN_ORG_ID and ARGUS_ATLASSIAN_TEAM_ID")
	}
	// An id travels inside a URL path. Anything that could end the path
	// segment is not an id at all, and refusing it here is cheaper than
	// reasoning about what a request built from it would ask for.
	for _, id := range []string{orgID, teamID} {
		if strings.ContainsAny(id, "/?#% ") || id != url.PathEscape(id) {
			return nil, errf("that is not an Atlassian id; check ARGUS_ATLASSIAN_ORG_ID and ARGUS_ATLASSIAN_TEAM_ID")
		}
	}
	if timeoutSeconds < 1 {
		timeoutSeconds = 45
	}
	return &TeamsClient{
		http: &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		host: teamsHost,
		auth: "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token)),
		org:  orgID,
		team: teamID,
	}, nil
}

func (c *TeamsClient) teamPath() string {
	return "/public/teams/v1/org/" + c.org + "/teams/" + c.team
}

// Team returns the team itself, which is how the panel can name what it
// is about to import from rather than showing a bare id.
func (c *TeamsClient) Team(ctx context.Context) (Team, error) {
	var t Team
	raw, err := c.do(ctx, http.MethodGet, c.teamPath(), nil)
	if err != nil {
		return Team{}, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return Team{}, errf("the Atlassian team API returned something that is not a team: %v", err)
	}
	return t, nil
}

// MemberIDs returns the account ids of everyone on the team.
//
// It is a POST rather than a GET. That is not a mistake in the request:
// the API answers 405 to a GET here, because the page size and the cursor
// are body fields.
func (c *TeamsClient) MemberIDs(ctx context.Context) ([]string, error) {
	var out []string
	cursor := ""
	for page := 0; page < maxMemberPages; page++ {
		body := map[string]any{"first": membersPageSize}
		if cursor != "" {
			body["after"] = cursor
		}
		raw, err := c.do(ctx, http.MethodPost, c.teamPath()+"/members", body)
		if err != nil {
			return nil, err
		}
		var answer struct {
			Results []struct {
				AccountID string `json:"accountId"`
			} `json:"results"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil {
			return nil, errf("the Atlassian team API returned something that is not a member list: %v", err)
		}
		for _, m := range answer.Results {
			if m.AccountID != "" {
				out = append(out, m.AccountID)
			}
		}
		if !answer.PageInfo.HasNextPage || answer.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = answer.PageInfo.EndCursor
	}
	// Reaching here means the API kept claiming another page. Returning
	// what was read is more useful than an error about a team nobody has.
	return out, nil
}

// assertTeamsRead is the allowlist. The paths are built privately, so
// this can never fail in practice - which is exactly why it is cheap to
// keep, and why a future write would have to be added deliberately.
func assertTeamsRead(method, path string) error {
	if method == http.MethodGet {
		return nil
	}
	if method == http.MethodPost && strings.HasSuffix(path, "/members") {
		return nil
	}
	return &WriteAttemptError{
		msg: fmt.Sprintf("%s on the Atlassian team API is not a known read; this client is read-only", method),
	}
}

func (c *TeamsClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	if err := assertTeamsRead(method, path); err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errf("encoding request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return nil, errf("building the request: %v", err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errf("could not reach the Atlassian team API: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errf("reading the answer from the Atlassian team API: %v", err)
	}
	if resp.StatusCode >= 400 {
		return nil, explainTeams(resp.StatusCode)
	}
	return raw, nil
}

// explainTeams says which setting or permission is wrong. It quotes
// neither the URL nor the response: the path carries the organisation and
// team ids, and the body can repeat them back.
func explainTeams(status int) error {
	switch status {
	case 401:
		return errf("Atlassian rejected the credentials (401). The team import uses the same " +
			"ARGUS_JIRA_EMAIL and ARGUS_JIRA_TOKEN as the rest of the sprint report.")
	case 403:
		return errf("Atlassian returned 403. The credentials are valid, but this account " +
			"cannot read that team's membership.")
	case 404:
		return errf("Atlassian returned 404. Either ARGUS_ATLASSIAN_ORG_ID or " +
			"ARGUS_ATLASSIAN_TEAM_ID does not match a team this account can see.")
	case 429:
		return errf("Atlassian is rate limiting the team API (429). Try the import again shortly.")
	}
	return errf("the Atlassian team API answered %d", status)
}
