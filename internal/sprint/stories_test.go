package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// story assembles a container and the work linked beneath it.
//
// The linked issues go into Inputs.Linked as well as into the links
// themselves, because that is how a report gets at their points: the stub
// Jira embeds in a link carries a status and a type and no custom fields
// at all.
type child struct {
	key    string
	status string
	points any
	doneAt time.Time
	kind   string
}

func withStory(t *testing.T, in *sprint.Inputs, key string, own any, status string, doneAt time.Time, children ...child) {
	t.Helper()

	links := make([]link, 0, len(children))
	for _, c := range children {
		kind := c.kind
		if kind == "" {
			kind = "Blocks"
		}
		links = append(links, link{kind: kind, key: c.key, issueType: "Task", status: c.status})

		kid := fixture{key: c.key, issueType: "Task", status: c.status, assignee: "a", points: c.points}.build(t)
		in.Linked[c.key] = kid
		if !c.doneAt.IsZero() {
			in.Changes[c.key] = []jira.StatusChange{moved(c.doneAt, "In Progress", c.status)}
		}
	}

	st := fixture{key: key, issueType: "Story", status: status, points: own, links: links}.build(t)
	in.Issues = append(in.Issues, st)
	if !doneAt.IsZero() {
		in.Changes[key] = []jira.StatusChange{moved(doneAt, "In Progress", status)}
	}
}

func flag(t *testing.T, r sprint.Report, kind string) sprint.Flag {
	t.Helper()
	for _, f := range r.Flags {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("no %s flag among %d", kind, len(r.Flags))
	return sprint.Flag{}
}

func storyKeys(r sprint.Report) []string {
	out := make([]string, 0, len(r.Stories))
	for _, s := range r.Stories {
		out = append(out, s.Key)
	}
	return out
}

// ---- the four outcomes of comparing a Story with its work -------------

// They agree, so there is nothing to say. This is the common case, and
// the flag it replaced fired here too - on every Story carrying points,
// calling correct data a problem.
func TestAStoryThatAgreesWithItsWorkIsSilent(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 6.0, "Done", mid,
		child{key: "ABC-101", status: "Done", points: 4.0, doneAt: mid},
		child{key: "ABC-102", status: "Done", points: 2.0, doneAt: mid},
	)

	r := sprint.Build(in)
	for _, f := range r.Flags {
		switch f.Kind {
		case sprint.FlagStoryPointsMismatch, sprint.FlagStoryNothingSized, sprint.FlagStoryNoWork:
			t.Errorf("a Story agreeing with its work raised %s: %s", f.Kind, f.Message)
		}
	}
}

// The rollup and the work underneath disagree. One of the two is wrong,
// and both numbers are shown so somebody can tell which.
func TestAStoryThatDisagreesWithItsWorkNamesBothNumbers(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 10.0, "Done", mid,
		child{key: "ABC-101", status: "Done", points: 8.0, doneAt: mid},
		child{key: "ABC-102", status: "Done", points: 4.5, doneAt: mid},
	)

	f := flag(t, sprint.Build(in), sprint.FlagStoryPointsMismatch)
	if !strings.Contains(f.Message, "10.0") || !strings.Contains(f.Message, "12.5") {
		t.Errorf("message names neither figure: %s", f.Message)
	}
}

// A Story claiming points over work nobody sized. The claim cannot be
// checked against anything, which is the thing to fix.
func TestAStoryOverUnsizedWorkSaysSo(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 10.0, "Done", mid,
		child{key: "ABC-101", status: "Done", doneAt: mid},
		child{key: "ABC-102", status: "Done", doneAt: mid},
	)

	r := sprint.Build(in)
	f := flag(t, r, sprint.FlagStoryNothingSized)
	if !strings.Contains(f.Message, "10.0") {
		t.Errorf("message does not say what was claimed: %s", f.Message)
	}
	if n := flagCount(r, sprint.FlagStoryPointsMismatch); n != 0 {
		t.Error("unsized work is being reported as a mismatch, which would make the sum look like zero")
	}
}

// Finished with nothing linked underneath. It is not that the work is
// hard to find: there is no record of what was done. Never summed to
// zero, which would report the Story as agreeing with its children.
func TestAStoryWithNoWorkUnderneathIsAFindingAboutTheWork(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 5.0, "Done", mid)

	r := sprint.Build(in)
	f := flag(t, r, sprint.FlagStoryNoWork)
	if !strings.Contains(f.Message, "no defined work") {
		t.Errorf("message: %s", f.Message)
	}
	if n := flagCount(r, sprint.FlagStoryPointsMismatch); n != 0 {
		t.Error("a Story with nothing underneath is being compared against a sum of zero")
	}
	// It is still concluded: the Story is done and nothing contradicts it.
	if got := storyKeys(r); len(got) != 1 {
		t.Errorf("stories concluded = %v, want the Story counted as well as flagged", got)
	}
}

// ---- when a Story has actually finished --------------------------------

// A Story marked done over work still in flight has not finished.
// Somebody shut the parent early, and the sprint that closes the last of
// the work is the one that gets to claim it.
func TestAStoryIsNotConcludedWhileItsWorkIsOpen(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 5.0, "Done", mid,
		child{key: "ABC-101", status: "Done", points: 3.0, doneAt: mid},
		child{key: "ABC-102", status: "In Progress", points: 2.0},
	)

	r := sprint.Build(in)
	if got := storyKeys(r); len(got) != 0 {
		t.Errorf("stories concluded = %v, want none while work underneath is open", got)
	}
}

// The Story concluded when the last of it did, not when somebody ticked
// the parent.
func TestAStoryIsDatedByTheLastOfItsWorkToFinish(t *testing.T) {
	early := sprintOpens.AddDate(0, 0, -6)
	late := sprintOpens.AddDate(0, 0, 3)

	in := base(t)
	withStory(t, &in, "ABC-100", 5.0, "Done", early,
		child{key: "ABC-101", status: "Done", points: 3.0, doneAt: early},
		child{key: "ABC-102", status: "Done", points: 2.0, doneAt: late},
	)

	r := sprint.Build(in)
	if got := storyKeys(r); len(got) != 1 {
		t.Fatalf("stories concluded = %v, want the sprint the last item finished in to claim it", got)
	}
	if !r.Stories[0].ConcludedAt.Equal(late) {
		t.Errorf("concluded at %v, want %v", r.Stories[0].ConcludedAt, late)
	}

	// The sprint that saw the Story itself ticked does not get it.
	earlier := base(t)
	earlier.Sprint.StartDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -14)}
	earlier.Sprint.CompleteDate = jira.Time{Time: sprintOpens.AddDate(0, 0, -1)}
	earlier.Sprint.EndDate = earlier.Sprint.CompleteDate
	earlier.Issues = in.Issues
	earlier.Changes = in.Changes
	earlier.Linked = in.Linked
	if got := storyKeys(sprint.Build(earlier)); len(got) != 0 {
		t.Errorf("earlier sprint claimed %v although the work was not finished then", got)
	}
}

// All the work is finished and nobody shut the Story. Until somebody
// does, no sprint can count it - which is worth a minute's tidying.
func TestWorkFinishedUnderAnOpenStoryIsRaised(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 5.0, "In Progress", time.Time{},
		child{key: "ABC-101", status: "Done", points: 5.0, doneAt: mid},
	)

	r := sprint.Build(in)
	f := flag(t, r, sprint.FlagStoryWorkDoneNotShut)
	if !strings.Contains(f.Message, "ABC-100") {
		t.Errorf("message: %s", f.Message)
	}
	if got := storyKeys(r); len(got) != 0 {
		t.Errorf("stories concluded = %v, want an open Story not counted", got)
	}
}

// An open Story with nothing linked to it has no finished work to shut
// off, so there is nothing to say about it.
func TestAnOpenStoryWithNoWorkIsSilent(t *testing.T) {
	in := base(t)
	withStory(t, &in, "ABC-100", nil, "In Progress", time.Time{})

	r := sprint.Build(in)
	for _, f := range r.Flags {
		if f.Kind == sprint.FlagStoryWorkDoneNotShut || f.Kind == sprint.FlagStoryNoWork {
			t.Errorf("an open Story with nothing underneath raised %s", f.Kind)
		}
	}
}

// Which link types carry the association is configuration, and a project
// that has been migrated normally has two in use at once.
func TestTheConfiguredLinkTypesAreTheOnesThatCount(t *testing.T) {
	in := base(t)
	in.StoryLinkTypes = []string{"migration_parent"}
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 5.0, "Done", mid,
		child{key: "ABC-101", status: "In Progress", points: 5.0, kind: "Duplicate"},
	)

	r := sprint.Build(in)
	// The one link there is is of a type that carries nothing, so as far
	// as this team is concerned the Story has no work underneath it.
	if n := flagCount(r, sprint.FlagStoryNoWork); n != 1 {
		t.Errorf("%d no-work flags, want a link of an unconfigured type ignored", n)
	}
}

// ---- the invariant -----------------------------------------------------

// A Story's points are a rollup of the work beneath it. Counting them
// beside the Tasks they roll up pays twice for the same work, which is
// the whole reason containers are excluded - and it is the kind of thing
// that survives being obvious right up until somebody refactors.
func TestStoryPointsNeverReachDelivered(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 33.3, "Done", mid,
		child{key: "ABC-101", status: "Done", points: 4.0, doneAt: mid},
		child{key: "ABC-102", status: "Done", points: 2.0, doneAt: mid},
	)
	// The linked Tasks are in the sprint as well, which is the normal
	// arrangement and the one where double counting would happen.
	for _, key := range []string{"ABC-101", "ABC-102"} {
		in.Issues = append(in.Issues, in.Linked[key])
	}

	r := sprint.Build(in)

	if r.Summary.DeliveredTotal != 6 {
		t.Errorf("delivered = %v, want only the 6 points of Tasks underneath", r.Summary.DeliveredTotal)
	}
	var storyPoints float64
	for _, s := range r.Stories {
		storyPoints += s.Points
	}
	if storyPoints != 33.3 {
		t.Errorf("stories concluded = %v points, want the rollup reported in its own section", storyPoints)
	}
	for _, p := range r.People {
		if p.Delivered > 6 {
			t.Errorf("%s delivered %v, want no rollup in a person's figure", p.Name, p.Delivered)
		}
	}
	for _, e := range r.Epics {
		if e.Points > 6 {
			t.Errorf("epic %s has %v points, want no rollup in the delivery split", e.Name, e.Points)
		}
	}
	// Say/do counts the work, not the container above it.
	if r.Summary.Completed != 2 {
		t.Errorf("completed = %d, want the two Tasks and not the Story", r.Summary.Completed)
	}
}
