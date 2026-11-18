package sprint_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// lateInputs is a sprint holding one committed task, with one more task
// brought in by the late label. Both finish mid-sprint.
func lateInputs(t *testing.T) sprint.Inputs {
	t.Helper()
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	in.Issues = []jira.Issue{
		fixture{key: "ABC-1", issueType: "Task", status: "Done", assignee: "a", points: 3.0}.build(t),
		fixture{key: "ABC-2", issueType: "Task", status: "Done", assignee: "b", points: 2.0,
			created: sprintOpens.AddDate(0, 0, 1)}.build(t),
	}
	in.Changes["ABC-1"] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
	in.Changes["ABC-2"] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
	in.Late = map[string]bool{"ABC-2": true}
	in.LateLabel = "late-21"
	return in
}

func TestLateWorkIsDeliveredButOutsideSayDo(t *testing.T) {
	r := sprint.Build(lateInputs(t))

	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want 5: late work that finished is delivered", r.Summary.DeliveredTotal)
	}
	if r.Summary.Promised != 1 || r.Summary.Injected != 0 || r.Summary.Completed != 1 {
		t.Errorf("say/do = promised %d, injected %d, completed %d; want 1, 0, 1 - the late task is in none of them",
			r.Summary.Promised, r.Summary.Injected, r.Summary.Completed)
	}
	if r.Summary.AddedLate != 1 || len(r.Late) != 1 || r.Late[0].Key != "ABC-2" {
		t.Errorf("added late = %d %v, want ABC-2 alone", r.Summary.AddedLate, r.Late)
	}
	for _, p := range r.People {
		if p.AccountID == "b" {
			if !rowFor(t, p.Rows, "ABC-2").Late {
				t.Error("ABC-2 should be marked late on Person b's rows")
			}
		}
	}
}

// Without the label, the same task would have been injected: it was
// created after the sprint opened. That is the case the exclusion is for.
func TestWithoutTheLabelTheSameTaskIsInjected(t *testing.T) {
	in := lateInputs(t)
	in.Late = nil
	r := sprint.Build(in)
	if r.Summary.Injected != 1 || r.Summary.Completed != 2 {
		t.Errorf("injected %d, completed %d; want 1 and 2", r.Summary.Injected, r.Summary.Completed)
	}
}

// Work no sprint field claims counts in the labelled sprint even when it
// finished after it closed, and in full.
func TestTheLabelWinsOverTheDateForWorkNoSprintHolds(t *testing.T) {
	in := lateInputs(t)
	in.Changes["ABC-2"] = []jira.StatusChange{moved(sprintCloses.AddDate(0, 0, 4), "In Progress", "Done")}
	in.Issues[1] = fixture{key: "ABC-2", issueType: "Task", status: "Done", assignee: "b", points: 2.0,
		worklog: []logged{{who: "b", at: sprintOpens.AddDate(0, 0, -3), seconds: 3600}}}.build(t)

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 5 {
		t.Errorf("delivered = %v, want 5", r.Summary.DeliveredTotal)
	}
	if !r.Late[0].LateOverride || r.Late[0].Credited != 2 {
		t.Errorf("ABC-2 = %+v, want counted here by the label with its whole 2 points", r.Late[0])
	}
}

// A labelled issue another sprint holds stays with that sprint's dating,
// and the clash is raised.
func TestTheLabelDefersToAnotherSprintField(t *testing.T) {
	in := lateInputs(t)
	in.Changes["ABC-2"] = []jira.StatusChange{moved(sprintCloses.AddDate(0, 0, 4), "In Progress", "Done")}
	other, _ := json.Marshal([]map[string]any{{"id": 745, "name": "Sprint 22",
		"startDate": sprintCloses.Format("2006-01-02T15:04:05.000Z")}})
	in.Issues[1].Raw[sprintID] = other

	r := sprint.Build(in)
	if r.Summary.DeliveredTotal != 3 {
		t.Errorf("delivered = %v, want 3: Sprint 22 counts ABC-2", r.Summary.DeliveredTotal)
	}
	if f := flag(t, r, sprint.FlagLateElsewhere); !strings.Contains(f.Message, "Sprint 22") {
		t.Errorf("flag should name the sprint holding it: %s", f.Message)
	}
}

// A late Story is a container like any other unless the setting lets it
// count, and then only with nothing beneath it in the sprint.
func TestLateContainerCountsOnlyWithTheSetting(t *testing.T) {
	build := func(count bool, withChild bool) sprint.Report {
		in := base(t)
		mid := sprintOpens.AddDate(0, 0, 3)
		var links []link
		if withChild {
			links = []link{{kind: "Blocks", key: "ABC-11", issueType: "Task", status: "Done"}}
			in.Issues = append(in.Issues,
				fixture{key: "ABC-11", issueType: "Task", status: "Done", assignee: "a", points: 1.0}.build(t))
			in.Changes["ABC-11"] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
		}
		in.Issues = append(in.Issues,
			fixture{key: "ABC-10", issueType: "Story", status: "Done", assignee: "a", points: 3.0, links: links}.build(t))
		in.Changes["ABC-10"] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
		in.Late = map[string]bool{"ABC-10": true}
		in.LateContainersCount = count
		return sprint.Build(in)
	}

	if r := build(false, false); r.Summary.DeliveredTotal != 0 || len(r.Stories) != 1 {
		t.Errorf("setting off: delivered %v, stories %d; want 0 and the story concluded", r.Summary.DeliveredTotal, len(r.Stories))
	}
	r := build(true, false)
	if r.Summary.DeliveredTotal != 3 || len(r.Stories) != 0 {
		t.Errorf("setting on: delivered %v, stories %d; want 3 and no rollup", r.Summary.DeliveredTotal, len(r.Stories))
	}
	if r.Summary.Promised != 0 || r.Summary.Completed != 0 {
		t.Errorf("a late Story counted as work is still outside say/do: %+v", r.Summary)
	}
	if r := build(true, true); r.Summary.DeliveredTotal != 1 {
		t.Errorf("work beneath it is in the sprint, so it stays a rollup: delivered %v, want 1", r.Summary.DeliveredTotal)
	}
}

func TestStoriesPerPerson(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 3.0, "Done", mid, child{key: "ABC-101", status: "Done", points: 3.0, doneAt: mid})
	withStory(t, &in, "ABC-200", nil, "In Progress", mid, child{key: "ABC-201", status: "In Progress", points: 2.0})
	// The work itself, in the sprint and assigned to b.
	in.Issues = append(in.Issues,
		fixture{key: "ABC-101", issueType: "Task", status: "Done", assignee: "b", points: 3.0}.build(t),
		fixture{key: "ABC-201", issueType: "Task", status: "In Progress", assignee: "b", points: 2.0}.build(t))
	in.Changes["ABC-101"] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
	in.Changes["ABC-201"] = []jira.StatusChange{moved(mid, "To Do", "In Progress")}

	r := sprint.Build(in)
	var got []string
	for _, p := range r.People {
		if p.AccountID != "b" {
			continue
		}
		for _, s := range p.Stories {
			got = append(got, s.Key+map[bool]string{true: " concluded", false: " worked on"}[s.Concluded])
		}
	}
	want := "ABC-100 concluded,ABC-200 worked on"
	if strings.Join(got, ",") != want {
		t.Errorf("Person b's stories = %v, want %s", got, want)
	}
}

func TestPersonAndSprintLinksIncludeTheLabel(t *testing.T) {
	in := lateInputs(t)
	in.BaseURL = "https://yourcompany.atlassian.net"
	r := sprint.Build(in)

	if !strings.Contains(r.Sprint.IssuesJQL, "labels") || !strings.Contains(r.Sprint.IssuesJQL, "744") {
		t.Errorf("issues JQL = %s, want the sprint and its label", r.Sprint.IssuesJQL)
	}
	for _, p := range r.People {
		if p.AccountID != "b" {
			continue
		}
		if !strings.Contains(p.AssignedJQL, "WAS") || !strings.Contains(p.AssignedJQL, "DURING") {
			t.Errorf("assigned JQL = %s", p.AssignedJQL)
		}
		if !strings.Contains(p.CreditedJQL, "ABC-2") {
			t.Errorf("credited JQL = %s, want ABC-2", p.CreditedJQL)
		}
	}
}

func TestLateLabelFor(t *testing.T) {
	for _, c := range []struct {
		pattern string
		n       int
		want    string
	}{
		{"sprint-{n}-late", 23, "sprint-23-late"},
		{"", 23, ""},
		{"sprint-{n}-late", 0, ""},
		{"no-placeholder", 23, ""},
	} {
		if got := config.LateLabelFor(c.pattern, c.n); got != c.want {
			t.Errorf("LateLabelFor(%q, %d) = %q, want %q", c.pattern, c.n, got, c.want)
		}
	}
}

// Each story lists who worked beneath it and the points they were
// credited there; somebody assigned unsized work is listed at zero.
func TestStoryWorkNamesPeopleAndEffort(t *testing.T) {
	in := base(t)
	mid := sprintOpens.AddDate(0, 0, 3)
	withStory(t, &in, "ABC-100", 5.0, "Done", mid,
		child{key: "ABC-101", status: "Done", points: 3.0, doneAt: mid},
		child{key: "ABC-102", status: "Done", points: 2.0, doneAt: mid},
		child{key: "ABC-103", status: "Done", doneAt: mid})
	for _, f := range []fixture{
		{key: "ABC-101", issueType: "Task", status: "Done", assignee: "a", points: 3.0},
		{key: "ABC-102", issueType: "Task", status: "Done", assignee: "b", points: 2.0},
		{key: "ABC-103", issueType: "Task", status: "Done", assignee: "c"},
	} {
		in.Issues = append(in.Issues, f.build(t))
		in.Changes[f.key] = []jira.StatusChange{moved(mid, "In Progress", "Done")}
	}

	r := sprint.Build(in)
	if len(r.StoryWork) != 1 {
		t.Fatalf("story work = %+v, want ABC-100 alone", r.StoryWork)
	}
	st := r.StoryWork[0]
	if st.Key != "ABC-100" || !st.Concluded || st.Points != 5 {
		t.Errorf("story = %+v, want ABC-100 concluded with 5 points of work", st)
	}
	var got []string
	for _, p := range st.People {
		got = append(got, p.Name+"="+strings.TrimRight(strings.TrimRight(strconvF(p.Points), "0"), "."))
	}
	if strings.Join(got, ",") != "Person a=3,Person b=2,Person c=0" {
		t.Errorf("people = %v", got)
	}
}

func strconvF(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }
