package sprint

import (
	"errors"
	"fmt"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// jiraStarted is the timestamp layout Jira's worklog endpoint accepts.
// It is RFC 3339 with the colon missing from the offset, and Jira rejects
// the form with the colon.
const jiraStarted = "2006-01-02T15:04:05.000-0700"

// Propose turns a report and a person's answers into a change set. Like
// Build it is a pure function of its inputs: nothing is read and nothing
// is written, and every request ends up either as a Change or as a Skip
// with its reason.
//
// The report says what may be changed and the issues say what the change
// is guarded against. Report deliberately carries neither the issue id
// nor fields.updated, and should not start to.
func Propose(rep Report, issues []jira.Issue, in ProposalInputs) ChangeSet {
	cs := ChangeSet{
		SprintJiraID: rep.Sprint.JiraID,
		BuiltAt:      in.Now,
		ExpiresAt:    in.Now.Add(ChangeSetLifetime),
		ReportBuilt:  rep.GeneratedAt,
		Actor:        in.Actor,
		// Slices rather than nil: this crosses to a browser as JSON, and
		// null where an array was promised is a crash in the panel.
		Changes: []Change{},
		Skipped: []Skip{},
	}
	p := proposer{
		in:      in,
		rows:    indexRows(rep),
		flagged: indexFlags(rep),
		issues:  make(map[string]jira.Issue, len(issues)),
		people:  indexPeople(in.People),
		seen:    map[string]bool{},
	}
	for _, is := range issues {
		p.issues[is.Key] = is
	}
	for _, req := range in.Requests {
		change, err := p.one(req)
		if err != nil {
			cs.Skipped = append(cs.Skipped, Skip{Key: req.Key, Op: req.Op, Person: req.Person, Reason: err.Error()})
			continue
		}
		cs.Changes = append(cs.Changes, change)
	}
	cs.Digest = Digest(cs.Changes)
	return cs
}

// proposer carries the indexes one Propose call works from.
type proposer struct {
	in      ProposalInputs
	rows    map[string]Row
	flagged map[string]bool // keys a flag names, whether or not a row shows them
	issues  map[string]jira.Issue
	people  map[string]store.Person

	// seen is what has already been proposed, so a second request for
	// the same thing is a skip rather than a second write.
	seen map[string]bool
}

// indexRows gathers every row the report shows, by key.
func indexRows(rep Report) map[string]Row {
	rows := map[string]Row{}
	for _, person := range rep.People {
		for _, row := range person.Rows {
			rows[row.Key] = row
		}
	}
	for _, row := range rep.Carry {
		rows[row.Key] = row
	}
	return rows
}

// indexFlags gathers the keys the flags name. A finished ticket with
// nobody assigned or no points is credited to nobody, so it appears in
// no person's rows; the flag is the only place the report mentions it,
// and it is exactly the ticket somebody wants to fix.
func indexFlags(rep Report) map[string]bool {
	keys := map[string]bool{}
	for _, f := range rep.Flags {
		if f.Key != "" {
			keys[f.Key] = true
		}
		for _, k := range f.Keys {
			keys[k] = true
		}
	}
	return keys
}

// one judges a single request. An error is the reason it is skipped, in
// words for the person who typed it.
func (p proposer) one(req ChangeRequest) (Change, error) {
	if _, inRows := p.rows[req.Key]; !inRows && !p.flagged[req.Key] {
		return Change{}, fmt.Errorf("%s is not in this sprint's report", req.Key)
	}
	is, ok := p.issues[req.Key]
	if !ok {
		return Change{}, fmt.Errorf("%s is in the report but not among the sprint's issues", req.Key)
	}
	switch req.Op {
	case OpPointsSet:
		return p.points(req, is)
	case OpAssigneeSet:
		return p.assignee(req, is)
	case OpWorklogAdd, OpWorklogUpdate, OpWorklogDelete:
		return p.worklog(req, is)
	}
	return Change{}, fmt.Errorf("%q is not an operation this tool knows", req.Op)
}

// once records that something has been proposed and refuses it a second
// time. The key is whatever makes two requests the same write.
func (p proposer) once(what, reason string) error {
	if p.seen[what] {
		return errors.New(reason)
	}
	p.seen[what] = true
	return nil
}

// change starts a Change from the issue, which is where the guard values
// come from. The row is used for the link where the report has one.
func (p proposer) change(req ChangeRequest, is jira.Issue, was any) Change {
	return Change{
		Key: is.Key, URL: p.rows[is.Key].URL, Summary: is.Fields.Summary,
		Type: is.Fields.IssueType.Name, Status: is.Fields.Status.Name, Op: req.Op,
		Guard: Guard{
			IssueID: is.ID, Updated: is.Fields.Updated.Time,
			Was: was, Status: is.Fields.Status.Name,
		},
	}
}

func (p proposer) points(req ChangeRequest, is jira.Issue) (Change, error) {
	if !p.in.Rules.ExpectsPointsWhenDone(is.Fields.IssueType.Name) {
		return Change{}, fmt.Errorf("%s is a %s, whose points are a rollup of its work rather than an estimate",
			is.Key, is.Fields.IssueType.Name)
	}
	if n, has := is.Number(p.in.PointsField); has {
		return Change{}, fmt.Errorf("%s already has %s points; only an empty field is written", is.Key, figure(n))
	}
	if req.Points == nil || *req.Points <= 0 {
		return Change{}, errors.New("story points must be a number above zero")
	}
	if err := p.once(is.Key+" "+req.Op, is.Key+" already has points proposed above"); err != nil {
		return Change{}, err
	}
	c := p.change(req, is, nil)
	c.Field = p.in.PointsField
	c.After = *req.Points
	c.AfterLabel = figure(*req.Points)
	c.Reason = "finished with no estimate"
	if !p.in.Rules.IsDone(is.Fields.Status.Name) {
		c.Reason = "open with no estimate"
	}
	return c, nil
}

func (p proposer) assignee(req ChangeRequest, is jira.Issue) (Change, error) {
	if who := is.Fields.Assignee; who != nil && who.AccountID != "" {
		return Change{}, fmt.Errorf("%s is already assigned to %s", is.Key, who.DisplayName)
	}
	person, err := p.onRoster(req.Assignee, "assignee")
	if err != nil {
		return Change{}, err
	}
	if err := p.once(is.Key+" "+req.Op, is.Key+" already has an assignee proposed above"); err != nil {
		return Change{}, err
	}
	c := p.change(req, is, nil)
	c.Field = "assignee"
	// Exactly the shape the gate accepts: an account id, never a name,
	// because names are not unique and Jira would guess.
	c.After = map[string]string{"accountId": person.AccountID}
	c.AfterLabel = person.Name
	c.Reason = "finished unassigned"
	if !p.in.Rules.IsDone(is.Fields.Status.Name) {
		c.Reason = "open and unassigned"
	}
	return c, nil
}

// onRoster resolves an account id to a person on the roster. Somebody who
// has left is on the roster for history's sake and is refused here: work
// is not assigned to, or logged for, a person who is not on the team.
func (p proposer) onRoster(accountID, what string) (store.Person, error) {
	if accountID == "" {
		return store.Person{}, fmt.Errorf("no %s was chosen", what)
	}
	person, ok := p.people[accountID]
	if !ok {
		return store.Person{}, fmt.Errorf("%s is not on the roster", accountID)
	}
	if !person.Active {
		return store.Person{}, fmt.Errorf("%s is on the roster but marked as having left", person.Name)
	}
	return person, nil
}
