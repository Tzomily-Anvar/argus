package backlog

import (
	"fmt"
	"sort"
	"strings"
)

// Group is one of the punch lists: which tickets, why the reader should
// care, and the JQL that opens the same set in the issue navigator.
//
// Where the set is expressible in JQL the query says what the group
// says, so the two can be checked against each other. Where it is not -
// acknowledgement is local, and a link to a container is not a JQL
// clause - the query names the keys, which opens exactly the set shown
// and nothing when it is empty.
type Group struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Why   string   `json:"why"`
	Keys  []string `json:"keys"`
	JQL   string   `json:"jql"`
}

// Groups draws every punch list over the rows. The roster is the
// operations account ids, for the query that names them.
func Groups(rows []Row, cfg Config, f Fields, roster map[string]bool) []Group {
	base := fmt.Sprintf("project = %s AND statusCategory != Done", cfg.Project)
	epics := quoteList(sortedKeys(f.EpicTypes))
	notEpic := "issuetype not in (" + epics + ")"
	work := "issuetype not in subTaskIssueTypes() AND " + notEpic
	if len(cfg.ContainerTypes) > 0 {
		work += " AND issuetype not in (" + quoteList(cfg.ContainerTypes) + ")"
	}
	isWork := func(r Row) bool {
		return !r.Subtask && !f.EpicTypes[r.Type] && !hasFold(cfg.ContainerTypes, r.Type)
	}
	isEpic := func(r Row) bool { return f.EpicTypes[r.Type] }

	out := []Group{
		{ID: "new_unacknowledged", Label: "New",
			Why:  fmt.Sprintf("Created in the last %d days and not yet acknowledged; acknowledged ones return when they change.", cfg.NewDays),
			Keys: pick(rows, func(r Row) bool { return r.New && !r.Acknowledged })},
		{ID: "stale", Label: "Stale",
			Why:  fmt.Sprintf("No update in %d days or more: forgotten, or quietly done.", cfg.StaleDays),
			Keys: pick(rows, func(r Row) bool { return r.StaleDays > 0 }),
			JQL:  fmt.Sprintf("%s AND updated <= -%dd", base, cfg.StaleDays)},
		{ID: "no_epic", Label: "No epic",
			Why:  "Work with no epic above it never shows in the delivery split.",
			Keys: pick(rows, func(r Row) bool { return !r.Subtask && !isEpic(r) && r.Epic == nil }),
			JQL:  fmt.Sprintf("%s AND issuetype not in subTaskIssueTypes() AND %s AND parent IS EMPTY", base, notEpic)},
	}
	if f.HasContainer {
		out = append(out, Group{ID: "no_story", Label: "No story",
			Why:  "A task or bug linked to no story will never roll up as delivered work.",
			Keys: pick(rows, func(r Row) bool { return isWork(r) && r.Story == nil })})
	}
	out = append(out,
		Group{ID: "no_labels", Label: "No labels",
			Why:  "Nothing says what kind of work this is.",
			Keys: pick(rows, func(r Row) bool { return !isEpic(r) && len(r.Labels) == 0 }),
			JQL:  fmt.Sprintf("%s AND %s AND labels IS EMPTY", base, notEpic)},
		Group{ID: "unsized", Label: "Unsized",
			Why:  "A task or bug with neither points nor an estimate cannot be planned.",
			Keys: pick(rows, func(r Row) bool { return isWork(r) && r.Points == nil && r.Estimate == nil }),
			JQL:  base + " AND " + work + sizedClause(f)},
		Group{ID: "no_sprint", Label: "Not in a sprint",
			Why: "In no active or future sprint: the backlog proper.",
			Keys: pick(rows, func(r Row) bool {
				return !r.Subtask && !isEpic(r) && (r.Sprint == nil || r.Sprint.State == "closed")
			}),
			JQL: fmt.Sprintf("%s AND issuetype not in subTaskIssueTypes() AND %s AND (sprint IS EMPTY OR (sprint not in openSprints() AND sprint not in futureSprints()))", base, notEpic)},
	)
	if f.HasContainer {
		out = append(out, Group{ID: "build_epic_no_story", Label: "Build work without a story",
			Why: "Under a Build epic but linked to no story, so it will never count as delivered.",
			Keys: pick(rows, func(r Row) bool {
				return isWork(r) && r.Epic != nil && r.Story == nil && classOf(cfg, r.Epic.Summary) == "Build"
			})})
	}
	out = append(out,
		Group{ID: "operations_missing_label", Label: "Operations requests missing the label",
			Why:  fmt.Sprintf("An operations request by its reporter or a legacy label, without the %s label.", cfg.OperationsLabel),
			Keys: pick(rows, func(r Row) bool { return r.OperationsMissingLabel }),
			JQL:  operationsJQL(base, cfg, roster)},
		Group{ID: "legacy_label", Label: "Legacy label",
			Why:  fmt.Sprintf("Carries an older spelling of the operations label, to be replaced with %s.", cfg.OperationsLabel),
			Keys: pick(rows, func(r Row) bool { return r.LegacyLabel }),
			JQL:  legacyJQL(base, cfg)},
		Group{ID: "operations_work_label", Label: "Requests carrying a work label",
			Why:  "Reported by the operations roster but labelled as a kind of engineering work, which names the fix rather than the need.",
			Keys: pick(rows, func(r Row) bool { return r.WorkLabel })},
	)
	for i := range out {
		if out[i].JQL == "" {
			out[i].JQL = keysJQL(out[i].Keys)
		}
	}
	return out
}

func pick(rows []Row, keep func(Row) bool) []string {
	out := []string{}
	for _, r := range rows {
		if keep(r) {
			out = append(out, r.Key)
		}
	}
	return out
}

// sizedClause is the fields a sized ticket would carry, each in the
// cf[NNNNN] form the issue navigator accepts.
func sizedClause(f Fields) string {
	var parts []string
	for _, id := range []string{f.Points, f.Estimate} {
		if id != "" {
			parts = append(parts, " AND "+jqlField(id)+" IS EMPTY")
		}
	}
	return strings.Join(parts, "")
}

func legacyJQL(base string, cfg Config) string {
	if len(cfg.LegacyLabels) == 0 {
		return ""
	}
	return fmt.Sprintf("%s AND labels in (%s)", base, quoteList(cfg.LegacyLabels))
}

// operationsJQL names the two signals JQL can see - the legacy labels
// and the roster's reporters - and excludes tickets already labelled.
func operationsJQL(base string, cfg Config, roster map[string]bool) string {
	var signals []string
	if len(cfg.LegacyLabels) > 0 {
		signals = append(signals, "labels in ("+quoteList(cfg.LegacyLabels)+")")
	}
	if ids := sortedKeys(roster); len(ids) > 0 {
		signals = append(signals, "reporter in ("+quoteList(ids)+")")
	}
	if len(signals) == 0 {
		return ""
	}
	return fmt.Sprintf("%s AND (%s) AND (labels IS EMPTY OR labels not in (%s))",
		base, strings.Join(signals, " OR "), quoteList([]string{cfg.OperationsLabel}))
}

// keysJQL opens exactly these keys, or nothing at all.
func keysJQL(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return "key in (" + strings.Join(keys, ", ") + ")"
}

// jqlField turns a REST field id into the form JQL accepts, for the
// reason the sprint report gives: the navigator refuses customfield_NNNNN
// and takes cf[NNNNN].
func jqlField(fieldID string) string {
	if n, ok := strings.CutPrefix(fieldID, "customfield_"); ok {
		return "cf[" + n + "]"
	}
	return fieldID
}

func quoteList(items []string) string {
	q := make([]string, 0, len(items))
	for _, s := range items {
		q = append(q, `"`+strings.ReplaceAll(s, `"`, "")+`"`)
	}
	return strings.Join(q, ", ")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortStrings(s []string) { sort.Strings(s) }

// Epic is one epic with the open work beneath it and how much of that
// work is not yet refined.
type Epic struct {
	Key       string   `json:"key"`
	Summary   string   `json:"summary"`
	Class     string   `json:"class"`
	Open      int      `json:"open"`
	Unrefined int      `json:"unrefined"`
	Keys      []string `json:"keys"`
	URL       string   `json:"url"`
}

// Epics counts the open work under every epic: the open epics first, in
// the order Jira gave them, then any epic a row names that the list
// lacks - a closed epic with work still open beneath it, which is worth
// seeing rather than hiding.
func Epics(rows []Row, open []IssueRef, cfg Config) []Epic {
	index := map[string]int{}
	out := make([]Epic, 0, len(open))
	add := func(ref IssueRef) *Epic {
		if i, ok := index[ref.Key]; ok {
			return &out[i]
		}
		index[ref.Key] = len(out)
		out = append(out, Epic{Key: ref.Key, Summary: ref.Summary, Class: classOf(cfg, ref.Summary),
			Keys: []string{}, URL: cfg.BaseURL + "/browse/" + ref.Key})
		return &out[len(out)-1]
	}
	for _, ref := range open {
		add(ref)
	}
	for _, r := range rows {
		if r.Epic == nil {
			continue
		}
		e := add(*r.Epic)
		e.Open++
		e.Keys = append(e.Keys, r.Key)
		if strings.EqualFold(r.Status, "To Refine") || r.StatusCategory == "new" {
			e.Unrefined++
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Unrefined != out[j].Unrefined {
			return out[i].Unrefined > out[j].Unrefined
		}
		return out[i].Open > out[j].Open
	})
	return out
}

// OperationsView is every operations request, the ones without the
// label, and the ones still carrying a legacy one.
type OperationsView struct {
	AllKeys          []string `json:"all_keys"`
	MissingLabelKeys []string `json:"missing_label_keys"`
	LegacyLabelKeys  []string `json:"legacy_label_keys"`
	WorkLabelKeys    []string `json:"work_label_keys"`
	Roster           []Ref    `json:"roster"`
}

// Operations reads the flags Rows already judged.
func Operations(rows []Row) OperationsView {
	return OperationsView{
		AllKeys:          pick(rows, func(r Row) bool { return r.Operations }),
		MissingLabelKeys: pick(rows, func(r Row) bool { return r.OperationsMissingLabel }),
		LegacyLabelKeys:  pick(rows, func(r Row) bool { return r.LegacyLabel }),
		WorkLabelKeys:    pick(rows, func(r Row) bool { return r.WorkLabel }),
		Roster:           []Ref{},
	}
}
