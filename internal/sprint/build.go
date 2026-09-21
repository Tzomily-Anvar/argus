package sprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Inputs are everything Build needs. Keeping them explicit means the
// computation is a pure function of its inputs and can be tested without
// a Jira or a database anywhere near it.
type Inputs struct {
	Sprint      jira.Sprint
	Issues      []jira.Issue
	People      []store.Person
	Capacity    []store.Capacity
	Rules       Rules
	PointsField string
	BaseURL     string
	Project     string

	// EpicClasses maps a class name to a pattern matched against the epic
	// summary, so a team can split Run from Build however they label it.
	EpicClasses map[string]*regexp.Regexp

	// SprintLengthDays turns days off into a share of a baseline.
	SprintLengthDays int
}

// Build computes the report. It reads nothing and writes nothing.
func Build(in Inputs) Report {
	r := Report{
		Sprint:      sprintInfo(in),
		GeneratedAt: time.Now().UTC(),
	}

	people := indexPeople(in.People)
	capacity := indexCapacity(in.Capacity)
	closed := in.Sprint.Closed()

	// Accumulators.
	delivered := map[string]float64{}     // account id -> points
	rowsFor := map[string][]Row{}         // account id -> their issues
	epicPoints := map[string]*EpicGroup{} // epic key, or name -> group
	var unattributed float64

	for _, is := range in.Issues {
		row := toRow(is, in, closed)

		// Containers roll up their children, so their points are reported
		// as work concluded rather than counted as anyone's delivery.
		if in.Rules.IsContainer(row.Type) {
			if row.HasPoints {
				r.Flags = append(r.Flags, containerPointsFlag(row, in))
			}
			if row.Done {
				r.Stories = append(r.Stories, Story{
					Key: row.Key, Summary: row.Summary, Points: row.Points,
					Epic: row.Epic, URL: row.URL,
				})
			}
			continue
		}

		r.Summary.IssueCount++
		if !row.Done {
			r.Carry = append(r.Carry, row)
			continue
		}

		r.Summary.DoneCount++
		r.Summary.DeliveredTotal += row.Points

		if !row.HasPoints && in.Rules.ExpectsPointsWhenDone(row.Type) {
			r.Flags = append(r.Flags, noEstimateFlag(row, in))
		}

		if is.Fields.Assignee == nil {
			unattributed += row.Points
			r.Flags = append(r.Flags, unassignedFlag(row, in))
		} else {
			id := is.Fields.Assignee.AccountID
			delivered[id] += row.Points
			rowsFor[id] = append(rowsFor[id], row)
			if _, known := people[id]; !known {
				people[id] = store.Person{
					AccountID: id,
					Name:      is.Fields.Assignee.DisplayName,
				}
			}
		}

		if in.Rules.InSayDo(row.Type) {
			r.Summary.Completed++
		}
		addToEpic(epicPoints, row)
	}

	// Say/do: promised existed before the sprint started, injected arrived
	// during it. By ticket count rather than points, so the answer does
	// not depend on every estimate being right.
	for _, is := range in.Issues {
		typ := is.Fields.IssueType.Name
		if in.Rules.IsContainer(typ) || !in.Rules.InSayDo(typ) {
			continue
		}
		if !in.Sprint.StartDate.IsZero() && is.Fields.Created.After(in.Sprint.StartDate.Time) {
			r.Summary.Injected++
		} else {
			r.Summary.Promised++
		}
	}

	r.Summary.UnattributedPoints = unattributed
	r.People = buildPeople(people, delivered, rowsFor, capacity, in, &r)
	r.Epics = finishEpics(epicPoints, r.Summary.DeliveredTotal, in)

	for _, p := range r.People {
		r.Summary.BaselineTotal += p.Baseline
		r.Summary.CapacityTotal += p.Capacity
		r.Summary.PlannedDaysOff += p.PlannedDaysOff
		r.Summary.UnplannedDaysOff += p.UnplannedDaysOff
	}

	sortFlags(r.Flags)
	sort.SliceStable(r.Carry, func(i, j int) bool { return r.Carry[i].Key < r.Carry[j].Key })
	sort.SliceStable(r.Stories, func(i, j int) bool { return r.Stories[i].Points > r.Stories[j].Points })
	return r
}

func sprintInfo(in Inputs) SprintInfo {
	return SprintInfo{
		JiraID:      in.Sprint.ID,
		Number:      in.Sprint.Number,
		Name:        in.Sprint.Name,
		State:       in.Sprint.State,
		Starts:      in.Sprint.StartDate.Time,
		Ends:        in.Sprint.EndDate.Time,
		Provisional: in.Sprint.Provisional,
		BoardID:     in.Sprint.OriginBoardID,
		BrowseURL: fmt.Sprintf("%s/jira/software/c/projects/%s/boards/%d/reports/sprint-retrospective?sprint=%d",
			in.BaseURL, in.Project, in.Sprint.OriginBoardID, in.Sprint.ID),
	}
}

func toRow(is jira.Issue, in Inputs, closed time.Time) Row {
	pts, has := is.Number(in.PointsField)
	row := Row{
		Key:       is.Key,
		Summary:   is.Fields.Summary,
		Type:      is.Fields.IssueType.Name,
		Status:    is.Fields.Status.Name,
		Points:    pts,
		HasPoints: has,
		Done:      in.Rules.IsDone(is.Fields.Status.Name),
		Created:   is.Fields.Created.Time,
		Resolved:  is.Fields.Resolved.Time,
		URL:       browseURL(in.BaseURL, is.Key),
	}
	if is.Fields.Assignee != nil {
		row.Assignee = is.Fields.Assignee.DisplayName
	}
	if is.Fields.Parent != nil {
		row.Epic = is.Fields.Parent.Fields.Summary
		row.EpicKey = is.Fields.Parent.Key
	}
	return row
}

func indexPeople(list []store.Person) map[string]store.Person {
	out := make(map[string]store.Person, len(list))
	for _, p := range list {
		out[p.AccountID] = p
	}
	return out
}

func indexCapacity(list []store.Capacity) map[string]store.Capacity {
	out := make(map[string]store.Capacity, len(list))
	for _, c := range list {
		out[c.AccountID] = c
	}
	return out
}

func buildPeople(
	people map[string]store.Person,
	delivered map[string]float64,
	rowsFor map[string][]Row,
	capacity map[string]store.Capacity,
	in Inputs,
	r *Report,
) []Person {
	out := make([]Person, 0, len(people))
	for id, p := range people {
		cap := capacity[id]
		person := Person{
			AccountID:        id,
			Name:             p.Name,
			Baseline:         p.Baseline,
			Delivered:        delivered[id],
			PlannedDaysOff:   cap.PlannedDaysOff,
			UnplannedDaysOff: cap.UnplannedDaysOff,
			Note:             cap.Note,
			Registered:       p.Baseline > 0,
			Rows:             rowsFor[id],
		}

		// Capacity is the baseline less the share of the sprint the person
		// was away for.
		person.Capacity = person.Baseline
		if in.SprintLengthDays > 0 && person.Baseline > 0 {
			perDay := person.Baseline / float64(in.SprintLengthDays)
			person.Capacity = person.Baseline - perDay*cap.DaysOff()
			if person.Capacity < 0 {
				person.Capacity = 0
			}
		}
		person.Delta = person.Delivered - person.Capacity

		if !person.Registered && person.Delivered > 0 {
			r.Flags = append(r.Flags, Flag{
				Kind: FlagDeliveredByStranger,
				Message: fmt.Sprintf("%s delivered %.1f points but is not on the roster, so there is nothing to compare it against",
					person.Name, person.Delivered),
			})
		}
		if person.Registered && !cap.Reviewed {
			r.Flags = append(r.Flags, Flag{
				Kind:    FlagCapacityUnreviewed,
				Message: fmt.Sprintf("%s's capacity has not been reviewed for this sprint; it is the baseline by default", person.Name),
			})
		}
		out = append(out, person)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// addToEpic groups by the epic's key where there is one, so two epics
// that happen to share a summary stay two epics.
func addToEpic(groups map[string]*EpicGroup, row Row) {
	name, id := row.Epic, row.EpicKey
	if name == "" {
		name = "(no epic)"
	}
	if id == "" {
		id = "name:" + name
	}
	g := groups[id]
	if g == nil {
		g = &EpicGroup{Key: row.EpicKey, Name: name}
		groups[id] = g
	}
	g.Points += row.Points
	g.IssueCount++
}

func finishEpics(groups map[string]*EpicGroup, total float64, in Inputs) []EpicGroup {
	out := make([]EpicGroup, 0, len(groups))
	for _, g := range groups {
		for class, re := range in.EpicClasses {
			if re != nil && re.MatchString(g.Name) {
				g.Class = class
				break
			}
		}
		if total > 0 {
			g.Share = g.Points / total
		}
		// Work with no epic has no key, so it keeps no URL and is read
		// as the plain label it is.
		g.URL = browseURL(in.BaseURL, g.Key)
		out = append(out, *g)
	}
	// Largest first: the point of this section is where the sprint went.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ---- flags -----------------------------------------------------------

func noEstimateFlag(row Row, in Inputs) Flag {
	return Flag{
		Kind:    FlagDoneNoEstimate,
		Key:     row.Key,
		Message: fmt.Sprintf("%s (%s) finished with no estimate", row.Key, row.Type),
		URL:     row.URL,
		JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype != %s AND %s IS EMPTY AND status IN (%s)`,
			in.Sprint.ID, firstOr(in.Rules.Container, "Epic"), in.PointsField, quoteList(in.Rules.Done))),
	}
}

func unassignedFlag(row Row, in Inputs) Flag {
	return Flag{
		Kind:    FlagDoneUnassigned,
		Key:     row.Key,
		Message: unassignedMessage(row),
		URL:     row.URL,
		JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND assignee IS EMPTY AND status IN (%s)`,
			in.Sprint.ID, quoteList(in.Rules.Done))),
	}
}

// unassignedMessage avoids claiming points went missing when there were
// none to lose: the issue is still worth flagging, but for a different
// reason.
func unassignedMessage(row Row) string {
	if row.Points > 0 {
		return fmt.Sprintf("%s finished with nobody assigned, so its %.1f points are credited to no one",
			row.Key, row.Points)
	}
	return fmt.Sprintf("%s finished with nobody assigned", row.Key)
}

func containerPointsFlag(row Row, in Inputs) Flag {
	return Flag{
		Kind:    FlagContainerWithPoints,
		Key:     row.Key,
		Message: fmt.Sprintf("%s is a %s carrying %.1f points of its own; those points are a rollup and are not counted as capacity", row.Key, row.Type, row.Points),
		URL:     row.URL,
	}
}

// jqlLink builds a Jira search URL, so a number can be checked rather
// than believed.
func jqlLink(in Inputs, jql string) string {
	return in.BaseURL + "/issues/?jql=" + urlEscape(jql)
}

func quoteList(items []string) string {
	q := make([]string, 0, len(items))
	for _, s := range items {
		q = append(q, `"`+strings.ReplaceAll(s, `"`, "")+`"`)
	}
	return strings.Join(q, ", ")
}

func firstOr(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// sortFlags puts the kinds that need action before the informational ones.
func sortFlags(flags []Flag) {
	rank := map[string]int{
		FlagDoneUnassigned:      0,
		FlagDoneNoEstimate:      1,
		FlagDeliveredByStranger: 2,
		FlagContainerWithPoints: 3,
		FlagCapacityUnreviewed:  4,
	}
	sort.SliceStable(flags, func(i, j int) bool {
		if rank[flags[i].Kind] != rank[flags[j].Kind] {
			return rank[flags[i].Kind] < rank[flags[j].Kind]
		}
		return flags[i].Key < flags[j].Key
	})
}
