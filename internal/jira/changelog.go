package jira

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// StatusChange is one move of an issue between workflow statuses.
//
// Only the status field is kept. A changelog carries every edit anybody
// ever made - summaries, labels, story points - and none of the rest says
// anything about when work finished.
type StatusChange struct {
	At   time.Time
	From string
	To   string

	// FromID and ToID are Jira's numeric status ids. The name says which
	// status, the id is what resolves to a category, and a workflow can
	// be renamed without the id moving. FromID is what categorises the
	// stretch before the first recorded change - the status the issue was
	// created in, which no later change mentions again.
	FromID string
	ToID   string
}

// changeTime is the timestamp on a changelog entry.
//
// The bulk endpoint answers with epoch milliseconds where the per-issue
// changelog answers with an ISO-8601 string, which is not something the
// documentation says and is only visible against a live site. Accepting
// both means a caller never has to know which endpoint it came from.
type changeTime struct{ time.Time }

func (t *changeTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		t.Time = time.UnixMilli(ms).UTC()
		return nil
	}
	var jt Time
	if err := jt.UnmarshalJSON(b); err != nil {
		return err
	}
	t.Time = jt.Time
	return nil
}

type changelogPage struct {
	IssueChangeLogs []struct {
		IssueID         string `json:"issueId"`
		ChangeHistories []struct {
			Created changeTime `json:"created"`
			Items   []struct {
				Field      string `json:"field"`
				From       string `json:"from"`
				FromString string `json:"fromString"`
				To         string `json:"to"`
				ToString   string `json:"toString"`
			} `json:"items"`
		} `json:"changeHistories"`
	} `json:"issueChangeLogs"`
	NextPageToken string `json:"nextPageToken"`
}

// changelogBatch is how many issues go into one bulkfetch request.
//
// It barely affects the number of round trips, because the endpoint pages
// by changelog entry rather than by issue - roughly a thousand entries per
// page whatever maxResults asks for - so asking about more issues at once
// simply moves the page boundary. A hundred keeps a single request small
// enough to reason about.
const changelogBatch = 100

// StatusHistory fetches the status transitions for a set of issues.
//
// The result is keyed by issue id and sorted oldest first, so a caller can
// replay an issue's life rather than guess at it from the one timestamp
// Jira keeps on the issue itself.
//
// Issues whose history is empty are absent from the map, which is not an
// error: an issue nobody ever moved has no transitions.
func (c *Client) StatusHistory(issueIDs []string) (map[string][]StatusChange, error) {
	out := make(map[string][]StatusChange, len(issueIDs))
	for start := 0; start < len(issueIDs); start += changelogBatch {
		end := start + changelogBatch
		if end > len(issueIDs) {
			end = len(issueIDs)
		}
		if err := c.statusHistoryBatch(issueIDs[start:end], out); err != nil {
			return nil, err
		}
	}
	for id := range out {
		changes := out[id]
		sort.SliceStable(changes, func(i, j int) bool { return changes[i].At.Before(changes[j].At) })
	}
	return out, nil
}

func (c *Client) statusHistoryBatch(ids []string, out map[string][]StatusChange) error {
	token := ""
	for page := 0; page < 50; page++ {
		// The status field alone. A changelog otherwise carries every
		// edit anybody ever made, and since the endpoint pages by entry
		// rather than by issue, asking for the rest doubles the number of
		// round trips to fetch data that is thrown away on arrival.
		body := map[string]any{"issueIdsOrKeys": ids, "fieldIds": []string{"status"}}
		if token != "" {
			body["nextPageToken"] = token
		}

		var res changelogPage
		if err := c.Post("/rest/api/3/changelog/bulkfetch", body, &res); err != nil {
			return errf("fetching changelogs for %d issues: %v", len(ids), err)
		}

		for _, entry := range res.IssueChangeLogs {
			for _, history := range entry.ChangeHistories {
				for _, item := range history.Items {
					if item.Field != "status" {
						continue
					}
					// Appended, never assigned. The endpoint pages by
					// changelog entry, so one issue's history can be split
					// across two pages and the second half would otherwise
					// replace the first.
					out[entry.IssueID] = append(out[entry.IssueID], StatusChange{
						At:     history.Created.Time,
						From:   item.FromString,
						To:     item.ToString,
						FromID: item.From,
						ToID:   item.To,
					})
				}
			}
		}

		if res.NextPageToken == "" {
			return nil
		}
		token = res.NextPageToken
	}
	return nil
}

// StatusCategories maps a status id to Jira's category for it: new,
// indeterminate or done.
//
// Categories are what separate "nobody picked this up" from "somebody was
// working on it", which no amount of status naming can be relied on to
// say. Which statuses mean finished is still the team's own list, by
// name - see Rules.Done - because that is the one Jira gets wrong.
func (c *Client) StatusCategories() (map[string]string, error) {
	var statuses []struct {
		ID       string `json:"id"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	if err := c.Get("/rest/api/3/status", nil, &statuses); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(statuses))
	for _, s := range statuses {
		out[s.ID] = s.Category.Key
	}
	return out, nil
}
