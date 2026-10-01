package backlog

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// Ref names a person the way a row needs to: the account id to act on
// and a label to read.
type Ref struct {
	AccountID string `json:"account_id"`
	Label     string `json:"label"`
}

// IssueRef names another issue: the epic above a ticket, the story it
// belongs to.
type IssueRef struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

// SprintRef is one sprint as the picker and the rows show it.
type SprintRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// Row is one open ticket with everything the views judge it by. The
// facts come from Jira; the flags at the end are judged here, against
// the thresholds, the acknowledgements and the requesters.
type Row struct {
	Key            string     `json:"key"`
	Summary        string     `json:"summary"`
	Type           string     `json:"type"`
	Subtask        bool       `json:"subtask"`
	Status         string     `json:"status"`
	StatusCategory string     `json:"status_category"`
	Created        time.Time  `json:"created"`
	Updated        time.Time  `json:"updated"`
	Reporter       Ref        `json:"reporter"`
	Assignee       *Ref       `json:"assignee"`
	Labels         []string   `json:"labels"`
	Epic           *IssueRef  `json:"epic"`
	Story          *IssueRef  `json:"story"`
	Points         *float64   `json:"points"`
	Estimate       *float64   `json:"estimate"`
	Sprint         *SprintRef `json:"sprint"`
	URL            string     `json:"url"`
	Priority       string     `json:"priority"`

	// Watermark is the updated timestamp exactly as Jira gave it, which
	// is what an acknowledgement records and compares against.
	Watermark string `json:"watermark"`

	New          bool `json:"new"`
	Acknowledged bool `json:"acknowledged"`
	StaleDays    int  `json:"stale_days"`
	// Request is a ticket from outside the team, by any of three signals:
	// the request label, a legacy spelling of it, or a reporter among the
	// requesters. RequestMissingLabel is one the label does not cover.
	Request             bool `json:"request"`
	RequestMissingLabel bool `json:"request_missing_label"`
	LegacyLabel         bool `json:"legacy_label"`
	// WorkLabel is a requester's ticket carrying a label that marks
	// engineering work. Judged on the reporter alone: an engineer may
	// label their own ticket however the team labels work.
	WorkLabel bool `json:"request_work_label"`
}

// Rows turns a sweep's issues into rows, judged against now, the
// acknowledgements (key to watermark) and the requesters (account ids).
func Rows(snap Snapshot, cfg Config, acks map[string]string, requesters map[string]bool, now time.Time) []Row {
	out := make([]Row, 0, len(snap.Issues))
	for _, is := range snap.Issues {
		out = append(out, rowOf(is, snap.Fields, cfg, acks, requesters, now))
	}
	return out
}

func rowOf(is jira.Issue, f Fields, cfg Config, acks map[string]string, requesters map[string]bool, now time.Time) Row {
	r := Row{
		Key: is.Key, Summary: is.Fields.Summary,
		Type: is.Fields.IssueType.Name, Subtask: is.Fields.IssueType.Subtask,
		Status: is.Fields.Status.Name, StatusCategory: is.Fields.Status.Category.Key,
		Created: is.Fields.Created.Time, Updated: is.Fields.Updated.Time,
		Labels:    is.Fields.Labels,
		URL:       cfg.BaseURL + "/browse/" + is.Key,
		Priority:  rawName(is, "priority"),
		Watermark: rawString(is, "updated"),
	}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	if u := is.Fields.Reporter; u != nil {
		r.Reporter = Ref{AccountID: u.AccountID, Label: u.DisplayName}
	}
	if u := is.Fields.Assignee; u != nil {
		r.Assignee = &Ref{AccountID: u.AccountID, Label: u.DisplayName}
	}
	if p := is.Fields.Parent; p != nil && f.EpicTypes[p.Fields.IssueType.Name] {
		r.Epic = &IssueRef{Key: p.Key, Summary: p.Fields.Summary}
	}
	r.Story = storyOf(is, cfg)
	if n, ok := is.Number(f.Points); ok {
		r.Points = &n
	}
	if n, ok := is.Number(f.Estimate); ok {
		r.Estimate = &n
	}
	// The sprint field is cumulative and the last entry is the current
	// one; a closed sprint there means the ticket rolled out of it.
	if sprints := is.SprintsOn(f.Sprint); len(sprints) > 0 {
		last := sprints[len(sprints)-1]
		r.Sprint = &SprintRef{ID: last.ID, Name: last.Name, State: last.State}
	}

	r.New = !r.Created.IsZero() && now.Sub(r.Created) <= time.Duration(cfg.NewDays)*24*time.Hour
	r.Acknowledged = r.Watermark != "" && acks[r.Key] == r.Watermark
	if !r.Updated.IsZero() {
		if days := int(now.Sub(r.Updated).Hours() / 24); days >= cfg.StaleDays {
			r.StaleDays = days
		}
	}
	r.LegacyLabel = hasAnyFold(r.Labels, cfg.LegacyLabels)
	labelled := hasFold(r.Labels, cfg.RequestLabel)
	r.Request = labelled || r.LegacyLabel || requesters[r.Reporter.AccountID]
	r.RequestMissingLabel = r.Request && !labelled
	r.WorkLabel = requesters[r.Reporter.AccountID] && hasAnyFold(r.Labels, cfg.WorkLabels)
	return r
}

// storyOf finds the container this ticket is linked beneath: a link of
// one of the story types whose far end is a container-type issue. The
// direction of the link is ignored, for the reason IssueLink.Other gives.
func storyOf(is jira.Issue, cfg Config) *IssueRef {
	for _, link := range is.Fields.Links {
		if !hasFold(cfg.StoryLinkTypes, link.Type.Name) {
			continue
		}
		other := link.Other()
		if other == nil || other.Key == "" || !hasFold(cfg.ContainerTypes, other.Fields.IssueType.Name) {
			continue
		}
		return &IssueRef{Key: other.Key, Summary: other.Fields.Summary}
	}
	return nil
}

// rawString reads a string field the typed Issue does not carry as one.
func rawString(is jira.Issue, field string) string {
	raw, ok := is.Raw[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// rawName reads the name out of an object field such as priority.
func rawName(is jira.Issue, field string) string {
	raw, ok := is.Raw[field]
	if !ok {
		return ""
	}
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v.Name
}

// hasFold reports whether name is in list, ignoring case and space.
func hasFold(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func hasAnyFold(list, names []string) bool {
	for _, n := range names {
		if hasFold(list, n) {
			return true
		}
	}
	return false
}

// classOf names the epic class a summary falls in, in a fixed order so
// two patterns that both match give the same answer every time.
func classOf(cfg Config, summary string) string {
	names := make([]string, 0, len(cfg.EpicClasses))
	for name := range cfg.EpicClasses {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		if re := cfg.EpicClasses[name]; re != nil && re.MatchString(summary) {
			return name
		}
	}
	return ""
}
