package sprint

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// mergeLate adds the issues a sprint's late label found to the issues the
// sprint itself holds, once each, and says which keys the label brought.
//
// An issue both in the sprint and labelled is kept as the sprint's own:
// it was committed to through the board like any other, so the label
// changes nothing about how it is counted. Only an issue the sprint does
// not hold is marked late.
func mergeLate(issues, labelled []jira.Issue) ([]jira.Issue, map[string]bool) {
	if len(labelled) == 0 {
		return issues, nil
	}
	have := make(map[string]bool, len(issues))
	for _, is := range issues {
		have[is.Key] = true
	}
	late := map[string]bool{}
	out := issues
	for _, is := range labelled {
		if have[is.Key] {
			continue
		}
		have[is.Key] = true
		late[is.Key] = true
		out = append(out, is)
	}
	return out, late
}

// jqlString quotes a value for JQL, escaping what would end the string.
func jqlString(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}

// sprintScope is the JQL for every issue the report reads: the sprint's
// own, and the late label's where there is one.
func sprintScope(in Inputs) string {
	if in.LateLabel == "" {
		return fmt.Sprintf("sprint = %d", in.Sprint.ID)
	}
	return fmt.Sprintf("(sprint = %d OR labels = %s)", in.Sprint.ID, jqlString(in.LateLabel))
}

// assignedDuring is every issue in the sprint somebody was assigned at
// any point while it ran. Whole days, because JQL reads a time in the
// searcher's own zone and a day either side is safer than an hour off.
func assignedDuring(in Inputs, accountID string, opens, closes time.Time) string {
	if opens.IsZero() || closes.IsZero() {
		return fmt.Sprintf("%s AND assignee WAS %s", sprintScope(in), jqlString(accountID))
	}
	return fmt.Sprintf(`%s AND assignee WAS %s DURING ("%s", "%s")`, sprintScope(in), jqlString(accountID),
		opens.UTC().Format("2006/01/02"), closes.UTC().AddDate(0, 0, 1).Format("2006/01/02"))
}

// creditedJQL is exactly the issues behind somebody's delivered figure.
func creditedJQL(in Inputs, rows []Row) string {
	if len(rows) == 0 {
		return ""
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.Key
	}
	sort.Strings(keys)
	return jqlLink(in, fmt.Sprintf("key in (%s) ORDER BY key", strings.Join(keys, ", ")))
}

// lateElsewhereFlag names a late-labelled issue the label could not
// bring here: it finished outside this sprint and another sprint's
// field holds it, so that sprint's dating decides.
func lateElsewhereFlag(row Row, is jira.Issue, in Inputs) Flag {
	var names []string
	for _, sp := range is.SprintsOn(in.SprintField) {
		names = append(names, sp.Name)
	}
	return Flag{
		Kind: FlagLateElsewhere,
		Key:  row.Key,
		URL:  row.URL,
		Message: fmt.Sprintf("%s carries %s but finished outside this sprint and is on %s, so it is counted there; "+
			"remove the label or the sprint", row.Key, in.LateLabel, strings.Join(names, ", ")),
	}
}

// storyTally works out, per person, the stories they had a hand in: those
// they worked on beneath, by credit or by being assigned work that
// finished or moved here, and those that concluded here in their name.
type storyTally struct {
	in        Inputs
	parents   map[string][]string // work key -> container keys
	info      map[string]PersonStory
	concluded map[string]bool
	touched   map[string]map[string]bool // account id -> container keys

	// effort is the points credited beneath each container, by person,
	// for everybody who worked beneath it. Not the same set as touched:
	// a story concluding in somebody's name is theirs to list, not work
	// they did beneath it.
	effort map[string]map[string]float64 // container key -> account id -> points
	names  map[string]string
	epics  map[string]string
}

func newStoryTally(in Inputs, work map[string][]workItem) *storyTally {
	t := &storyTally{in: in, parents: map[string][]string{}, info: map[string]PersonStory{},
		concluded: map[string]bool{}, touched: map[string]map[string]bool{},
		effort: map[string]map[string]float64{}, names: map[string]string{}, epics: map[string]string{}}
	for c, items := range work {
		for _, it := range items {
			t.parents[it.Key] = append(t.parents[it.Key], c)
		}
	}
	return t
}

func (t *storyTally) mark(id, key string) {
	if id == "" {
		return
	}
	if t.touched[id] == nil {
		t.touched[id] = map[string]bool{}
	}
	t.touched[id][key] = true
}

// markRow credits the stories above a row, and the row itself where it is
// a late container counted as work.
func (t *storyTally) markRow(id string, row Row, points float64) {
	if id == "" {
		return
	}
	stories := t.parents[row.Key]
	if t.in.lateWork[row.Key] {
		stories = append(append([]string{}, stories...), row.Key)
	}
	for _, c := range stories {
		t.mark(id, c)
		if t.effort[c] == nil {
			t.effort[c] = map[string]float64{}
		}
		t.effort[c][id] += points
	}
}

func (t *storyTally) container(is jira.Issue, row Row, here bool) {
	t.info[row.Key] = PersonStory{Key: row.Key, Summary: row.Summary, Status: row.Status, URL: row.URL}
	t.epics[row.Key] = row.Epic
	t.concluded[row.Key] = here
	if here && is.Fields.Assignee != nil {
		t.mark(is.Fields.Assignee.AccountID, row.Key)
	}
}

// work records a row the assignee had a hand in: it concluded here, or it
// was picked up here and carried on.
func (t *storyTally) work(is jira.Issue, row Row) {
	if t.in.lateWork[row.Key] {
		t.info[row.Key] = PersonStory{Key: row.Key, Summary: row.Summary, Status: row.Status, URL: row.URL}
		t.epics[row.Key] = row.Epic
		t.concluded[row.Key] = row.State == StateConcluded
	}
	if is.Fields.Assignee == nil {
		return
	}
	if row.State == StateConcluded || (row.State == StateCarried && row.Active) {
		t.names[is.Fields.Assignee.AccountID] = is.Fields.Assignee.DisplayName
		t.markRow(is.Fields.Assignee.AccountID, row, 0)
	}
}

func (t *storyTally) credit(id string, row Row, points float64) { t.markRow(id, row, points) }

// contributions lists every story somebody worked beneath, concluded ones
// first and then by the effort beneath them. names resolves an account
// id to the name the report uses for that person.
func (t *storyTally) contributions(names map[string]string) []StoryContribution {
	out := []StoryContribution{}
	for key, by := range t.effort {
		st, ok := t.info[key]
		if !ok {
			continue
		}
		c := StoryContribution{Key: key, Summary: st.Summary, Status: st.Status, URL: st.URL,
			Epic: t.epics[key], Concluded: t.concluded[key]}
		for id, pts := range by {
			name := names[id]
			if name == "" {
				name = t.names[id]
			}
			c.People = append(c.People, StoryPerson{AccountID: id, Name: name, Points: round2(pts)})
			c.Points += pts
		}
		c.Points = round2(c.Points)
		sort.Slice(c.People, func(i, j int) bool {
			if c.People[i].Points != c.People[j].Points {
				return c.People[i].Points > c.People[j].Points
			}
			return c.People[i].Name < c.People[j].Name
		})
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Concluded != out[j].Concluded {
			return out[i].Concluded
		}
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// of lists a person's stories, the concluded ones first.
func (t *storyTally) of(id string) []PersonStory {
	out := []PersonStory{}
	for key := range t.touched[id] {
		st, ok := t.info[key]
		if !ok {
			continue
		}
		st.Concluded = t.concluded[key]
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Concluded != out[j].Concluded {
			return out[i].Concluded
		}
		return out[i].Key < out[j].Key
	})
	return out
}
