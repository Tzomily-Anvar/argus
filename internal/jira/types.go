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
}

// Status is an issue's workflow status.
type Status struct {
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
		Parent    *struct {
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
