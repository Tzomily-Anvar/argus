package sprint

import (
	"sort"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// closer carries the indexes one Closeout call works from.
type closer struct {
	in     CloseoutInputs
	issues map[string]jira.Issue
	labels map[string]string // account id to name, from the report then the roster
	roster []Assignable      // the active roster, by name
}

// finishedHere reports whether an issue reached Done inside the stretch
// the report claims, which is the same question toRow answers with
// StateConcluded and the same reading of the history.
//
// It is dated again rather than read off the report because the report
// has nowhere to show a finished ticket that earned nothing: no points and
// no time logged is a flag, but no points with time logged is credited
// zero and appears in no list at all - and that is precisely a ticket the
// size step exists for.
func (c closer) finishedHere(is jira.Issue, rep Report) bool {
	life := newTimeline(is, c.in.Changes[is.ID], nil, c.in.Rules.Done)
	at := concluded(is, Inputs{}, life)
	if at.IsZero() {
		return false
	}
	return !at.Before(rep.Sprint.CountsFrom) && !at.After(rep.Sprint.CountsUntil)
}

// size offers a finished ticket whose points field is empty. The field
// itself is read rather than the row's HasPoints, because a row whose
// points came from the estimate reads as sized and is precisely a row
// this step exists for.
func (c closer) size(is jira.Issue) (SizeRow, bool) {
	typ := is.Fields.IssueType.Name
	if !c.in.Rules.ExpectsPointsWhenDone(typ) {
		return SizeRow{}, false
	}
	if _, has := is.Number(c.in.PointsField); has {
		return SizeRow{}, false
	}
	row := SizeRow{
		Key: is.Key, Summary: is.Fields.Summary, Type: typ, Status: is.Fields.Status.Name,
		URL: browseURL(c.in.BaseURL, is.Key),
	}
	// Only when the two fields are genuinely different: one field doing
	// both jobs would have the ticket suggesting its own empty value.
	if c.in.EstimateField != "" && c.in.EstimateField != c.in.PointsField {
		if est, has := is.Number(c.in.EstimateField); has {
			row.Estimate = &est
			row.Suggested = &est
		}
	}
	return row, true
}

// assign offers a finished ticket with nobody assigned, suggesting the
// person who moved it to Done if that person is on the active roster.
func (c closer) assign(is jira.Issue) (AssignRow, bool) {
	if who := is.Fields.Assignee; who != nil && who.AccountID != "" {
		return AssignRow{}, false
	}
	row := AssignRow{
		Key: is.Key, Summary: is.Fields.Summary, Type: is.Fields.IssueType.Name,
		Status: is.Fields.Status.Name, URL: browseURL(c.in.BaseURL, is.Key),
		Candidates: c.roster,
	}
	if who := finisher(c.in.Changes[is.ID], c.in.Rules); who != nil {
		for _, cand := range c.roster {
			if cand.AccountID == who.AccountID {
				row.SuggestedAccountID = cand.AccountID
				row.SuggestedLabel = cand.Label
			}
		}
	}
	return row, true
}

// finisher is who moved the issue into the Done run it is still in: the
// author of the first change of the final unbroken stretch of Done
// statuses, which is the same reading concludedAt uses for when. A later
// tidy from one Done status to another is not who finished it.
func finisher(changes []jira.StatusChange, rules Rules) *jira.User {
	var who *jira.User
	for i := len(changes) - 1; i >= 0; i-- {
		if !rules.IsDone(changes[i].To) {
			break
		}
		who = changes[i].Author
	}
	return who
}

// effort is a carried ticket with what is logged against it now.
func (c closer) effort(row Row, is jira.Issue) EffortRow {
	out := EffortRow{
		Key: row.Key, Summary: row.Summary, Status: row.Status, URL: row.URL,
		HoursLogged: row.HoursLogged,
		Entries:     worklogViews(is, c.labels),
	}
	if who := is.Fields.Assignee; who != nil {
		out.AssigneeAccountID = who.AccountID
		out.AssigneeLabel = who.DisplayName
		if out.AssigneeLabel == "" {
			out.AssigneeLabel = c.labels[who.AccountID]
		}
	}
	return out
}

// stories lists the containers whose work is all done: what the report
// concluded here, and any other container in a Done status over finished
// work. Each carries the sum beneath it and whether that sum is known.
func (c closer) stories(rep Report) []StoryRollup {
	work := linkedWork(Inputs{
		Issues: c.in.Issues, Rules: c.in.Rules, StoryLinkTypes: c.in.StoryLinkTypes,
		Linked: c.in.Linked, Changes: c.in.Changes, PointsField: c.in.PointsField,
	})
	concluded := map[string]bool{}
	for _, st := range rep.Stories {
		concluded[st.Key] = true
	}

	out := []StoryRollup{}
	for _, is := range c.in.Issues {
		if !c.in.Rules.IsContainer(is.Fields.IssueType.Name) {
			continue
		}
		items := work[is.Key]
		shut := c.in.Rules.IsDone(is.Fields.Status.Name) && len(items) > 0 && allDone(items)
		if !concluded[is.Key] && !shut {
			continue
		}
		linked, sized := pointsUnderneath(items)
		row := StoryRollup{
			Key: is.Key, Summary: is.Fields.Summary, URL: browseURL(c.in.BaseURL, is.Key),
			LinkedCount: len(items), LinkedPoints: linked,
			SumKnown: len(items) > 0 && sized == len(items),
		}
		if own, has := is.Number(c.in.PointsField); has {
			row.OwnPoints = &own
		}
		if row.SumKnown {
			row.Suggested = &linked
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
