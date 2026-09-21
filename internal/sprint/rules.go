// Package sprint turns a sprint's issues into a report.
//
// The rules below describe how one team uses Jira, and they are the whole
// reason the numbers come out right. They are named for what they do
// rather than for some general theory of agile, because the general
// theory is not yet known - a second team will bend these into a better
// shape than guessing now would produce.
package sprint

import "strings"

// Rules describe a team's conventions. Every one of them is configuration
// rather than code, because every one of them differs between teams and
// getting one wrong silently changes the headline number.
type Rules struct {
	// Done are the status names that count as delivered. By name, not by
	// Jira's done category: teams usually have several statuses in that
	// category and disagree about which of them mean finished.
	Done []string

	// Container types hold a rollup of the work beneath them. Their points
	// are a total of their children, so counting them as capacity would
	// count the same work twice. They are reported separately instead, as
	// work concluded.
	//
	// Provisional name. It describes one team's Story-over-Task
	// arrangement; another may put the rollup on an Epic, or have none.
	Container []string

	// EstimatedOnResolve types get their points when the work finishes
	// rather than when it is planned - recording what something actually
	// cost rather than what it was expected to. An open one without points
	// is correct; a finished one without points is a gap.
	EstimatedOnResolve []string

	// ExcludedFromSayDo types carry no commitment of their own, so they
	// are left out of the promised-versus-delivered count.
	ExcludedFromSayDo []string
}

func has(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), name) {
			return true
		}
	}
	return false
}

// IsDone reports whether a status name counts as delivered.
func (r Rules) IsDone(status string) bool { return has(r.Done, status) }

// IsContainer reports whether an issue type rolls up its children.
func (r Rules) IsContainer(issueType string) bool { return has(r.Container, issueType) }

// CountsTowardCapacity reports whether an issue type's points are part of
// what a person delivered. Containers are the exception.
func (r Rules) CountsTowardCapacity(issueType string) bool { return !r.IsContainer(issueType) }

// ExpectsPointsWhenDone reports whether a finished issue of this type
// should carry an estimate. Everything does except containers, whose
// points are a rollup and optional.
func (r Rules) ExpectsPointsWhenDone(issueType string) bool { return !r.IsContainer(issueType) }

// ExpectsPointsWhenOpen reports whether an issue of this type should
// already carry an estimate before it is finished. Types estimated on
// resolve should not.
func (r Rules) ExpectsPointsWhenOpen(issueType string) bool {
	return !r.IsContainer(issueType) && !has(r.EstimatedOnResolve, issueType)
}

// InSayDo reports whether an issue type counts toward say/do.
func (r Rules) InSayDo(issueType string) bool { return !has(r.ExcludedFromSayDo, issueType) }
