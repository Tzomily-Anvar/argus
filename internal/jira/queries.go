package jira

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var trailingNumber = regexp.MustCompile(`(\d+)\s*$`)

// Fields fetches the field catalogue, so a custom field can be found by
// name. Field ids differ per Jira site, which is why none can be
// hardcoded and why the id is configurable.
func (c *Client) Fields() ([]Field, error) {
	var fields []Field
	if err := c.Get("/rest/api/3/field", nil, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// FieldID resolves a custom field id from its display name. Returns ""
// when no field has that name.
func (c *Client) FieldID(name string) (string, error) {
	fields, err := c.Fields()
	if err != nil {
		return "", err
	}
	for _, f := range fields {
		if strings.EqualFold(f.Name, name) {
			return f.ID, nil
		}
	}
	return "", nil
}

// User resolves an account id to the person behind it.
//
// The Atlassian team API answers with account ids and nothing else, so
// this is what turns an import into a list of names somebody can read and
// tick. It also says whether the account is still active, which is worth
// knowing before adding it to a roster.
func (c *Client) User(accountID string) (User, error) {
	var u User
	if accountID == "" {
		return u, errf("no account id to look up")
	}
	err := c.Get("/rest/api/3/user", url.Values{"accountId": {accountID}}, &u)
	return u, err
}

// Search runs a JQL query and follows pagination.
//
// Jira's newer search endpoint pages with an opaque token rather than an
// offset, so pages cannot be fetched out of order or in parallel - the
// next token is only known once the current page has come back.
func (c *Client) Search(jql string, fields []string, limit int) ([]Issue, error) {
	var out []Issue
	token := ""

	for page := 0; page < 50; page++ {
		body := map[string]any{
			"jql":        jql,
			"fields":     fields,
			"maxResults": 100,
		}
		if token != "" {
			body["nextPageToken"] = token
		}

		var res SearchResult
		if err := c.Post("/rest/api/3/search/jql", body, &res); err != nil {
			return nil, fmt.Errorf("searching %q: %w", jql, err)
		}
		out = append(out, res.Issues...)

		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
		if res.IsLast || res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	return out, nil
}

// SprintByID fetches one sprint from the Agile API.
func (c *Client) SprintByID(id int64) (Sprint, error) {
	var s Sprint
	if err := c.Agile("/sprint/"+strconv.FormatInt(id, 10), nil, &s); err != nil {
		return Sprint{}, err
	}
	s.Number = numberFromName(s.Name)
	s.Provisional = s.CompleteDate.IsZero()
	return s, nil
}

// BoardSprints lists a board's sprints, newest first.
func (c *Client) BoardSprints(boardID int64, states string) ([]Sprint, error) {
	var page struct {
		Values []Sprint `json:"values"`
	}
	params := url.Values{"maxResults": {"50"}}
	if states != "" {
		params.Set("state", states)
	}
	if err := c.Agile("/board/"+strconv.FormatInt(boardID, 10)+"/sprint", params, &page); err != nil {
		return nil, err
	}
	for i := range page.Values {
		page.Values[i].Number = numberFromName(page.Values[i].Name)
		page.Values[i].Provisional = page.Values[i].CompleteDate.IsZero()
	}
	// Newest first, because every caller wants recent sprints.
	for i, j := 0, len(page.Values)-1; i < j; i, j = i+1, j-1 {
		page.Values[i], page.Values[j] = page.Values[j], page.Values[i]
	}
	return page.Values, nil
}

// ResolveSprint finds a sprint for a project.
//
// With no number it takes the open sprint; with one it looks for a sprint
// whose name ends in that number. The lookup goes through an issue rather
// than the board API because a project's board id is not something the
// caller should have to know - one issue in the sprint carries it.
func (c *Client) ResolveSprint(project, sprintField string, number int) (Sprint, error) {
	jql := fmt.Sprintf("project = %s AND sprint in openSprints() ORDER BY updated DESC", quote(project))
	if number > 0 {
		jql = fmt.Sprintf("project = %s AND sprint in (%d) ORDER BY updated DESC", quote(project), number)
	}

	issues, err := c.Search(jql, []string{sprintField}, 1)
	if err != nil || len(issues) == 0 {
		if number > 0 {
			// The sprint id and the sprint number are different things, so
			// searching by number only works if the caller passed an id.
			// Fall back to matching on the name.
			return c.sprintByNumber(project, sprintField, number)
		}
		if err != nil {
			return Sprint{}, err
		}
		return Sprint{}, errf("no open sprint found in project %s", project)
	}

	entry, ok := sprintEntry(issues[0], sprintField, number)
	if !ok {
		return Sprint{}, errf("issue %s carries no sprint in field %s", issues[0].Key, sprintField)
	}
	return c.SprintByID(entry)
}

// sprintByNumber searches by sprint name when an id lookup finds nothing.
func (c *Client) sprintByNumber(project, sprintField string, number int) (Sprint, error) {
	jql := fmt.Sprintf(`project = %s AND sprint = "Sprint %d" ORDER BY updated DESC`, quote(project), number)
	issues, err := c.Search(jql, []string{sprintField}, 1)
	if err != nil {
		return Sprint{}, err
	}
	if len(issues) == 0 {
		return Sprint{}, errf("no sprint numbered %d found in project %s", number, project)
	}
	entry, ok := sprintEntry(issues[0], sprintField, number)
	if !ok {
		return Sprint{}, errf("issue %s carries no sprint in field %s", issues[0].Key, sprintField)
	}
	return c.SprintByID(entry)
}

// sprintEntry pulls a sprint id out of an issue's sprint field, which is
// an array because an issue can span several sprints. When a number is
// given the matching one wins; otherwise the last, which is the current.
func sprintEntry(issue Issue, sprintField string, number int) (int64, bool) {
	raw, ok := issue.Raw[sprintField]
	if !ok {
		return 0, false
	}
	var entries []Sprint
	if err := jsonUnmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return 0, false
	}
	if number > 0 {
		for _, e := range entries {
			if numberFromName(e.Name) == number || e.ID == int64(number) {
				return e.ID, true
			}
		}
	}
	return entries[len(entries)-1].ID, true
}

// trailingDigits is the sprint number people use, which Jira keeps only
// as the tail of the name.
func trailingDigits(name string) int { return numberFromName(name) }

func numberFromName(name string) int {
	m := trailingNumber.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// quote guards a project key going into JQL. Keys are short and
// alphanumeric, so anything else is rejected rather than escaped.
func quote(project string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		}
		return -1
	}, project)
	return clean
}
