package sprint

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// The compare-and-set half of the apply: the issue as Jira holds it now,
// against the guard the preview recorded. Pure, so it can be tested
// against constructed issues without a Jira anywhere near it.

// conflict says in plain words what moved since the preview, or nothing
// if nothing did. A zero or nil part of the guard is not compared: a
// reversal is built from the audit log rather than from a Jira read, so
// it can only guard on what the log holds, which is the value Argus
// itself wrote.
//
// touched and added are what this same apply has already done to the
// issue. Its own earlier write moved updated and may have added an entry,
// and a second change on the same issue is not a conflict with the first.
func conflict(c Change, is jira.Issue, points string, rules Rules, touched bool, added []string) string {
	if !touched && !c.Guard.Updated.IsZero() && !is.Fields.Updated.Time.Equal(c.Guard.Updated) {
		return fmt.Sprintf("edited in Jira since the preview (at %s, previewed at %s)",
			is.Fields.Updated.Format(time.RFC3339), c.Guard.Updated.Format(time.RFC3339))
	}
	switch c.Op {
	case OpPointsSet:
		n, has := is.Number(points)
		want, wantHas := asNumber(c.Guard.Was)
		if has != wantHas || (has && n != want) {
			return fmt.Sprintf("the points field now holds %s, not %s", orEmpty(held(n, has)), orEmpty(held(want, wantHas)))
		}
		return statusLeftDone(c, is, rules)
	case OpAssigneeSet:
		var now string
		if is.Fields.Assignee != nil {
			now = is.Fields.Assignee.AccountID
		}
		if want := accountOf(c.Guard.Was); now != want {
			return fmt.Sprintf("the assignee is now %s, not %s", orEmpty(now), orEmpty(want))
		}
		return statusLeftDone(c, is, rules)
	case OpWorklogAdd:
		want, known := idList(c.Guard.Was)
		if !known {
			return ""
		}
		var now []string
		for _, w := range is.Fields.Worklog.Entries {
			if !contains(added, w.ID) {
				now = append(now, w.ID)
			}
		}
		sort.Strings(now)
		sort.Strings(want)
		if strings.Join(now, ",") != strings.Join(want, ",") {
			return fmt.Sprintf("time was logged on the issue since the preview (%d entries now, %d then)", len(now), len(want))
		}
	case OpWorklogUpdate, OpWorklogDelete:
		var entry *jira.WorklogEntry
		for i := range is.Fields.Worklog.Entries {
			if is.Fields.Worklog.Entries[i].ID == c.WorklogID {
				entry = &is.Fields.Worklog.Entries[i]
			}
		}
		if entry == nil {
			return fmt.Sprintf("worklog entry %s is no longer on the issue", c.WorklogID)
		}
		if g := c.Guard.Entry; g != nil {
			if !g.Updated.IsZero() && !entry.Updated.Time.Equal(g.Updated) {
				return fmt.Sprintf("worklog entry %s was edited since the preview", c.WorklogID)
			}
			if entry.Seconds != g.Seconds {
				return fmt.Sprintf("worklog entry %s now holds %sh, not the %sh previewed",
					c.WorklogID, figure(float64(entry.Seconds)/3600), figure(float64(g.Seconds)/3600))
			}
		}
	}
	return ""
}

// statusLeftDone catches a finished ticket that has been reopened: the
// preview offered its points or assignee because it was done, and it no
// longer is. A ticket that was open at preview time is not held to it.
func statusLeftDone(c Change, is jira.Issue, rules Rules) string {
	if rules.IsDone(c.Guard.Status) && !rules.IsDone(is.Fields.Status.Name) {
		return fmt.Sprintf("status moved from %s to %s since the preview", c.Guard.Status, is.Fields.Status.Name)
	}
	return ""
}

// ---- reading guard values, which arrive as any ------------------------

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// held prints a number the log or a message can carry: the figure, or
// nothing at all for a field that is empty.
func held(n float64, has bool) string {
	if !has {
		return ""
	}
	return figure(n)
}

func orEmpty(s string) string {
	if s == "" {
		return "empty"
	}
	return s
}

// accountOf reads an account id from the shapes an assignee travels in:
// the {"accountId": ...} body the gate accepts, or a bare id.
func accountOf(v any) string {
	switch a := v.(type) {
	case string:
		return a
	case map[string]string:
		return a["accountId"]
	case map[string]any:
		if id, ok := a["accountId"].(string); ok {
			return id
		}
	}
	return ""
}

// idList reads the worklog ids a guard recorded. known is false when the
// guard holds no expectation, which is how a reversal is built.
func idList(v any) (ids []string, known bool) {
	switch l := v.(type) {
	case []string:
		return append([]string{}, l...), true
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				ids = append(ids, s)
			}
		}
		return ids, true
	}
	return nil, false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
