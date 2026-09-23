package sprint

import (
	"context"
	"fmt"
	"sort"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The close-out is one sitting with several jobs in it: size what
// finished without points, assign what finished without an owner, log
// effort on what was carried, roll Stories up, then explain and publish.
// The model below is the checklist those steps render from, each row
// pre-filled with the best suggestion the report can make and labelled
// as a suggestion. Nothing here writes; every row feeds the same queue,
// preview and apply as the inline shortcuts do.

// CloseoutInputs are everything Closeout needs beyond the report. Kept
// explicit, like Inputs and ProposalInputs, so the computation is a pure
// function and can be tested without a Jira or a database anywhere near
// it.
type CloseoutInputs struct {
	Issues  []jira.Issue
	Changes map[string][]jira.StatusChange // by issue id, oldest first
	People  []store.Person                 // the roster, active and not
	Rules   Rules
	BaseURL string

	// Linked holds the issues beneath this sprint's containers, and
	// StoryLinkTypes says which links tie them. Both are what the report
	// itself was built from; without them a Story's sum cannot be told
	// from an unsized one.
	Linked         map[string]jira.Issue
	StoryLinkTypes []string

	PointsField   string
	EstimateField string
	HoursPerPoint float64
}

// CloseoutModel is the checklist, one list per step. Every list is a
// slice rather than nil because it crosses to a browser as JSON.
type CloseoutModel struct {
	SprintJiraID int64 `json:"sprint_jira_id"`
	Sprint       int   `json:"sprint"`

	// HoursPerPoint is stated so the effort step can show the points
	// beside the hours typed, which is the rule for that form.
	HoursPerPoint float64 `json:"hours_per_point"`

	Size    []SizeRow        `json:"size"`
	Assign  []AssignRow      `json:"assign"`
	Effort  []EffortRow      `json:"effort"`
	Stories []StoryRollup    `json:"stories"`
	People  []CloseoutPerson `json:"people"`
}

// SizeRow is a finished piece of work with no points. Suggested is the
// refinement estimate where one was recorded, and null otherwise: Argus
// never invents a number, so a row with no estimate is typed or skipped.
type SizeRow struct {
	Key       string   `json:"key"`
	Summary   string   `json:"summary"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	URL       string   `json:"url"`
	Estimate  *float64 `json:"estimate"`
	Suggested *float64 `json:"suggested"`
}

// Assignable is one person the assign step can pick.
type Assignable struct {
	AccountID string `json:"account_id"`
	Label     string `json:"label"`
}

// AssignRow is a finished piece of work with nobody assigned. The
// suggestion is whoever moved it into a Done status, provided they are on
// the active roster; "leave it" is always offered and is the client's to
// render.
type AssignRow struct {
	Key                string       `json:"key"`
	Summary            string       `json:"summary"`
	Type               string       `json:"type"`
	Status             string       `json:"status"`
	URL                string       `json:"url"`
	SuggestedAccountID string       `json:"suggested_account_id"`
	SuggestedLabel     string       `json:"suggested_label"`
	Candidates         []Assignable `json:"candidates"`
}

// EffortRow is a carried ticket somebody worked on, with what is logged
// against it now. The assignee is the suggested person; the hours are
// left blank, because that is the one figure only a person knows.
type EffortRow struct {
	Key               string        `json:"key"`
	Summary           string        `json:"summary"`
	Status            string        `json:"status"`
	URL               string        `json:"url"`
	AssigneeAccountID string        `json:"assignee_account_id"`
	AssigneeLabel     string        `json:"assignee_label"`
	HoursLogged       float64       `json:"hours_logged"`
	Entries           []WorklogView `json:"entries"`
}

// StoryRollup is a container whose work is all done. Suggested is the sum
// of that work when every item beneath it is sized, and null when the sum
// is unknown - and then there is no write, because a rollup over unsized
// work would be a guess written into the velocity history.
type StoryRollup struct {
	Key          string   `json:"key"`
	Summary      string   `json:"summary"`
	URL          string   `json:"url"`
	OwnPoints    *float64 `json:"own_points"`
	LinkedCount  int      `json:"linked_count"`
	LinkedPoints float64  `json:"linked_points"`
	SumKnown     bool     `json:"sum_known"`
	Suggested    *float64 `json:"suggested"`
}

// CloseoutPerson is one measured person, for the Reasons step: the note
// as it stands is the pre-filled line.
type CloseoutPerson struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Note      string `json:"note"`
}

// Closeout builds the checklist from a report and what it was built
// from. Like Build it is a pure function of its inputs: nothing is read
// and nothing is written.
func Closeout(rep Report, in CloseoutInputs) CloseoutModel {
	m := CloseoutModel{
		SprintJiraID:  rep.Sprint.JiraID,
		Sprint:        rep.Sprint.Number,
		HoursPerPoint: in.HoursPerPoint,
		Size:          []SizeRow{},
		Assign:        []AssignRow{},
		Effort:        []EffortRow{},
		Stories:       []StoryRollup{},
		People:        []CloseoutPerson{},
	}
	c := closer{in: in, issues: make(map[string]jira.Issue, len(in.Issues)), labels: labelsOf(rep)}
	for _, is := range in.Issues {
		c.issues[is.Key] = is
	}
	for _, p := range in.People {
		if c.labels[p.AccountID] == "" {
			c.labels[p.AccountID] = p.Name
		}
		if p.Active {
			c.roster = append(c.roster, Assignable{AccountID: p.AccountID, Label: p.Name})
		}
	}
	sort.Slice(c.roster, func(i, j int) bool { return c.roster[i].Label < c.roster[j].Label })

	for _, is := range in.Issues {
		if in.Rules.IsContainer(is.Fields.IssueType.Name) || !c.finishedHere(is, rep) {
			continue
		}
		if row, ok := c.size(is); ok {
			m.Size = append(m.Size, row)
		}
		if row, ok := c.assign(is); ok {
			m.Assign = append(m.Assign, row)
		}
	}
	sort.Slice(m.Size, func(i, j int) bool { return m.Size[i].Key < m.Size[j].Key })
	sort.Slice(m.Assign, func(i, j int) bool { return m.Assign[i].Key < m.Assign[j].Key })

	for _, row := range rep.Carry {
		if is, ok := c.issues[row.Key]; ok && row.Active {
			m.Effort = append(m.Effort, c.effort(row, is))
		}
	}

	m.Stories = c.stories(rep)

	for _, p := range rep.People {
		if p.Measured {
			m.People = append(m.People, CloseoutPerson{AccountID: p.AccountID, Name: p.Name, Note: p.Note})
		}
	}
	return m
}

// Closeout builds the checklist for a sprint whose report is cached.
// Nothing is asked of Jira: the issues, their histories and the work
// beneath their containers all came in when the report was built, and
// the roster comes from the store. A sprint nobody has opened is
// ErrNoReport, for the same reason Propose says so.
func (s *Service) Closeout(ctx context.Context, sprintNumber int) (CloseoutModel, error) {
	entry, found := s.cachedSprint(sprintNumber)
	if !found {
		return CloseoutModel{}, fmt.Errorf("%w: sprint %d; open its report first", ErrNoReport, sprintNumber)
	}
	people, err := s.store.ListPeople(ctx, true)
	if err != nil {
		return CloseoutModel{}, err
	}
	in := CloseoutInputs{
		Issues: entry.issues, Changes: entry.changes, People: people,
		Rules: s.cfg.Rules, BaseURL: s.cfg.BaseURL,
		Linked: entry.linked, StoryLinkTypes: s.cfg.StoryLinkTypes,
		HoursPerPoint: s.cfg.HoursPerPoint,
	}
	if entry.fields != nil {
		in.PointsField = entry.fields.points
		in.EstimateField = entry.fields.estimate
	}
	return Closeout(entry.report, in), nil
}
