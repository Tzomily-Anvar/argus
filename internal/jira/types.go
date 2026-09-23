package jira

import (
	"encoding/json"
	"strings"
	"time"
)

// Time parses Jira's timestamps, which are ISO-8601 with a timezone
// offset that has no colon (2026-09-21T10:43:19.000+0200). Go's RFC3339
// rejects that, so it needs its own layout.
type Time struct{ time.Time }

const jiraLayout = "2006-01-02T15:04:05.000-0700"

func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range []string{jiraLayout, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return nil // an unparseable date is missing data, not a fatal error
}

func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time)
}

// Sprint is one sprint as Jira knows it.
type Sprint struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	State         string `json:"state"`
	StartDate     Time   `json:"startDate"`
	EndDate       Time   `json:"endDate"`
	CompleteDate  Time   `json:"completeDate"`
	OriginBoardID int64  `json:"originBoardId"`

	// Number is the trailing digits of the name, which is what people
	// call the sprint. Jira has no field for it.
	Number int `json:"-"`

	// Provisional is true when the sprint has not been closed yet, so
	// "delivered" is measured against now rather than a real close time.
	Provisional bool `json:"-"`
}

// Closed is when the sprint ended for the purpose of deciding what was
// delivered: its completion time if it has one, otherwise now.
func (s Sprint) Closed() time.Time {
	if !s.CompleteDate.IsZero() {
		return s.CompleteDate.Time
	}
	if !s.EndDate.IsZero() && s.EndDate.Before(time.Now()) {
		return s.EndDate.Time
	}
	return time.Now().UTC()
}

// User is a Jira account, reduced to what a report needs.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Active      bool   `json:"active"`

	// AccountType is atlassian for a person, app for an integration and
	// customer for a service desk account. It is absent from the user an
	// issue embeds and present when a user is fetched directly, which is
	// where it matters: an app that files tickets should not be proposed
	// as a member of the team.
	AccountType string `json:"accountType,omitempty"`
}

// IsPerson reports whether this account belongs to a human. An empty
// account type is read as a person, because the abbreviated user Jira
// embeds in an issue carries no type and inventing a robot from a missing
// field would be worse than the occasional bot in a list.
func (u User) IsPerson() bool {
	return u.AccountType == "" || u.AccountType == "atlassian"
}

// Status is an issue's workflow status.
type Status struct {
	// ID is what resolves to a category. A workflow can be renamed
	// without it moving, so it is the stable handle on a status.
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category struct {
		Key string `json:"key"` // new | indeterminate | done
	} `json:"statusCategory"`
}

// IssueType distinguishes an Epic from a Story from a Task.
type IssueType struct {
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

// Issue is one work item. Custom fields vary per site, so they are kept
// as raw JSON and read through the field ids resolved at startup.
type Issue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary   string    `json:"summary"`
		IssueType IssueType `json:"issuetype"`
		Status    Status    `json:"status"`
		Assignee  *User     `json:"assignee"`
		Reporter  *User     `json:"reporter"`
		Created   Time      `json:"created"`
		Updated   Time      `json:"updated"`
		Resolved  Time      `json:"resolutiondate"`
		Labels    []string  `json:"labels"`

		// TimeSpent is seconds logged against the issue, zero when
		// nobody has logged any. It is the cheap aggregate: answering
		// "has any time been logged" needs no worklog fetch.
		TimeSpent int `json:"timespent"`

		// Worklog is the time logged against the issue, entry by entry.
		// A search returns it inline, which is why splitting a sprint's
		// delivery by who logged the work costs no extra call. Total
		// above MaxResults means Jira truncated the list.
		Worklog struct {
			Total      int            `json:"total"`
			MaxResults int            `json:"maxResults"`
			Entries    []WorklogEntry `json:"worklogs"`
		} `json:"worklog"`

		// Links are the issue's links to other issues. Where a team's
		// hierarchy puts Tasks under a Story but Jira's parent field is
		// taken by the Epic, this is the only place the association
		// between a container and its work exists.
		Links []IssueLink `json:"issuelinks"`

		Parent *struct {
			Key    string `json:"key"`
			Fields struct {
				Summary   string    `json:"summary"`
				IssueType IssueType `json:"issuetype"`
			} `json:"fields"`
		} `json:"parent"`
	} `json:"fields"`

	// Raw carries the whole fields object, so a site-specific custom
	// field can be read without this struct having to know about it.
	Raw map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps both the typed view and the raw field map.
func (i *Issue) UnmarshalJSON(b []byte) error {
	type alias Issue
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*i = Issue(a)

	var envelope struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(b, &envelope); err == nil {
		i.Raw = envelope.Fields
	}
	return nil
}

// Number reads a numeric custom field, such as story points. Returns 0
// and false when the field is absent or null, which Jira uses for "not
// estimated" - distinct from an estimate of zero.
func (i Issue) Number(fieldID string) (float64, bool) {
	raw, ok := i.Raw[fieldID]
	if !ok || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

// SprintsOn lists every sprint the issue has been on the board of, read
// from the sprint custom field.
//
// The field is cumulative - Jira never removes a sprint from it - which
// is exactly why a report cannot take membership as evidence that work
// happened in a sprint. Each entry carries its own dates, so working out
// which of them an issue was actually worked on in costs no further call.
func (i Issue) SprintsOn(fieldID string) []Sprint {
	raw, ok := i.Raw[fieldID]
	if !ok || string(raw) == "null" {
		return nil
	}
	var entries []Sprint
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	for n := range entries {
		entries[n].Number = trailingDigits(entries[n].Name)
	}
	return entries
}

// IsDone reports whether the issue's status is one of the configured
// terminal names.
//
// Matching is by name rather than by Jira's "done" status category,
// because teams routinely have several statuses in that category and
// disagree about which of them means finished. A team treating "Ready for
// production" as delivered and "Done" as delivered, but not "Rejected",
// can only say so by name.
func (i Issue) IsDone(doneStatuses []string) bool {
	for _, s := range doneStatuses {
		if strings.EqualFold(strings.TrimSpace(s), i.Fields.Status.Name) {
			return true
		}
	}
	return false
}

// WorklogEntry is one entry of logged time.
//
// Author rather than assignee is the point of holding these: a ticket
// that spans sprints sits with whoever it was handed to last, which is
// often not who did the work.
//
// Author is not always who did the work either. Jira sets it to whoever
// made the request and offers no way to log time on somebody's behalf,
// so a lead closing out a sprint on the team's behalf authors every
// entry and names the real person with an @mention in the comment. The
// comment is kept, decoded no further than it has to be, so that
// convention can be read.
type WorklogEntry struct {
	ID      string          `json:"id"`
	Started Time            `json:"started"`
	Seconds int             `json:"timeSpentSeconds"`
	Author  *User           `json:"author"`
	Comment json.RawMessage `json:"comment,omitempty"`
}

// Truncated reports whether Jira cut the worklog list short, in which case
// the entries here are not the whole story.
func (i Issue) Truncated() bool {
	return i.Fields.Worklog.Total > len(i.Fields.Worklog.Entries)
}

// IssueLink is one link between two issues.
//
// Jira puts the issue at the far end in inwardIssue or outwardIssue
// depending on which way the link was made, and which way round it was
// made says nothing useful about whether a Task belongs to a Story. So
// callers ask for Other and ignore the direction.
type IssueLink struct {
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
	Inward  *LinkedIssue `json:"inwardIssue"`
	Outward *LinkedIssue `json:"outwardIssue"`
}

// Other is the issue at the far end of the link, nil for a link that
// carries neither side.
func (l IssueLink) Other() *LinkedIssue {
	if l.Outward != nil {
		return l.Outward
	}
	return l.Inward
}

// LinkedIssue is the abbreviated issue Jira embeds in a link. It carries
// a status and a type but no custom fields, so story points on a linked
// issue still have to be fetched.
type LinkedIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary   string    `json:"summary"`
		Status    Status    `json:"status"`
		IssueType IssueType `json:"issuetype"`
	} `json:"fields"`
}

// SearchResult is one page of a JQL search.
type SearchResult struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken"`
	IsLast        bool    `json:"isLast"`
}

// Field is an entry from the field catalogue, used to resolve a custom
// field id from its name.
type Field struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
}
