package sprint

import (
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// Jira's status categories. A team names its statuses however it likes,
// but every status sits in exactly one of these three, and that is the
// only thing that reliably separates "nobody has picked this up" from
// "somebody is working on it".
const (
	catNotStarted = "new"
	catInProgress = "indeterminate"
)

// spell is one stretch an issue spent in a single status.
//
// From is zero for the stretch before the first recorded change - the
// status the issue was created in - and To is zero for the stretch it is
// still in.
type spell struct {
	From, To time.Time
	Status   string
	StatusID string
}

// timeline is one issue's life, replayed from its status history.
//
// The issue itself carries one timestamp for finishing and Jira sets that
// only on some workflows, so the history is the only place a report can
// ask when work actually reached a given state.
type timeline struct {
	spells []spell
	done   []string
	cats   map[string]string
}

// newTimeline replays an issue's status history.
//
// The status before the first recorded change is the one that change
// moved away from, which is how the status an issue was created in gets a
// stretch at all. An issue nobody ever moved has a single open-ended
// stretch in the status it is in now.
func newTimeline(is jira.Issue, changes []jira.StatusChange, cats map[string]string, done []string) timeline {
	t := timeline{done: done, cats: cats}
	if len(changes) == 0 {
		t.spells = []spell{{Status: is.Fields.Status.Name, StatusID: is.Fields.Status.ID}}
		return t
	}
	if changes[0].From != "" {
		t.spells = append(t.spells, spell{
			To: changes[0].At, Status: changes[0].From, StatusID: changes[0].FromID,
		})
	}
	for i, c := range changes {
		s := spell{From: c.At, Status: c.To, StatusID: c.ToID}
		if i+1 < len(changes) {
			s.To = changes[i+1].At
		}
		t.spells = append(t.spells, s)
	}
	return t
}

// statusAt is the status the issue was in at a moment: the spell that
// contains it, or the last spell for a moment after the history ends.
// Empty before the first recorded spell began.
func (t timeline) statusAt(at time.Time) string {
	for _, s := range t.spells {
		if (s.From.IsZero() || !at.Before(s.From)) && (s.To.IsZero() || at.Before(s.To)) {
			return s.Status
		}
	}
	return ""
}

// concludedAt is when the issue reached the state it is still in, if that
// state is one the team calls finished. Zero when it is not finished now.
//
// It is the start of the final unbroken run of Done statuses, which is the
// only reading that survives both of the ways this goes wrong:
//
//   - A team with several Done statuses moves a ticket from the first of
//     them to the last days later, often in the next sprint. Dating it by
//     the last transition would hand the work to whichever sprint
//     happened to tidy the board. The run started when it first reached a
//     status the team calls finished, and that is when it concluded.
//   - A ticket that was finished, reopened and finished again broke its
//     run. It concluded when the final run began, not the first, so the
//     earlier sprint does not get to claim work that came back.
//
// Between them these are what stop the same work being counted in four
// sprints running.
func (t timeline) concludedAt() time.Time {
	var start time.Time
	for _, s := range t.spells {
		if has(t.done, s.Status) {
			if start.IsZero() {
				start = s.From
			}
			continue
		}
		start = time.Time{}
	}
	return start
}

// finished reports whether the issue is in a Done status now.
func (t timeline) finished() bool {
	if len(t.spells) == 0 {
		return false
	}
	return has(t.done, t.spells[len(t.spells)-1].Status)
}

// workedOnDuring reports whether the issue sat in an in-progress status at
// any point in the window.
//
// This is what separates a ticket somebody picked up and did not finish
// from a ticket that sat in the backlog column all sprint. Both are open
// at the end, only the first is work that happened.
func (t timeline) workedOnDuring(from, to time.Time) bool {
	for _, s := range t.spells {
		if !t.inProgress(s) {
			continue
		}
		if (s.To.IsZero() || s.To.After(from)) && (s.From.IsZero() || s.From.Before(to)) {
			return true
		}
	}
	return false
}

// startedBefore reports whether the issue was already being worked on
// before the given moment.
//
// A sprint that finishes work started weeks earlier should be able to say
// so. Counting the whole ticket there is the simpler and more defensible
// rule, but it does mean the figure includes effort spent before the
// sprint began, and a report should not pretend otherwise.
func (t timeline) startedBefore(at time.Time) bool {
	for _, s := range t.spells {
		if !t.inProgress(s) {
			continue
		}
		if s.From.IsZero() || s.From.Before(at) {
			return true
		}
	}
	return false
}

// inProgress reports whether a stretch was somebody working on the issue.
//
// Finished is decided by the team's own status names, because that is the
// judgement Jira's categories get wrong. Everything else falls back to the
// category, and a status id nobody recognises is read as in progress: the
// two states that can be positively identified are "not picked up" and
// "finished", and guessing the other way would quietly report a sprint in
// which nothing at all happened.
func (t timeline) inProgress(s spell) bool {
	if has(t.done, s.Status) {
		return false
	}
	return t.cats[s.StatusID] != catNotStarted
}
