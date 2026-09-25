package sprint

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Inputs are everything Build needs. Keeping them explicit means the
// computation is a pure function of its inputs and can be tested without
// a Jira or a database anywhere near it.
type Inputs struct {
	Sprint   jira.Sprint
	Issues   []jira.Issue
	People   []store.Person
	Capacity []store.Capacity
	Rules    Rules
	BaseURL  string
	Project  string

	// PointsField holds what the work actually cost. It is the figure a
	// sprint is measured by.
	PointsField string

	// EstimateField holds what the work was expected to cost. It is read
	// only when a finished ticket never had the actual filled in:
	// crediting zero for work that demonstrably happened is further from
	// the truth than using the number somebody wrote down beforehand. The
	// fallback is reported rather than quietly applied.
	EstimateField string

	// SprintField is the sprint custom field, used to find the other
	// sprints an issue has been on the board of.
	SprintField string

	// Changes is each issue's status history, keyed by issue id. Without
	// it nothing can be dated, and every issue that has ever touched the
	// sprint looks like this sprint's work.
	Changes map[string][]jira.StatusChange

	// StatusCategories maps a Jira status id to its category, which is
	// what says whether a ticket was ever picked up.
	StatusCategories map[string]string

	// Linked holds the issues linked to this sprint's containers, keyed
	// by issue key, including ones not in the sprint themselves. A Story
	// cannot be judged finished without them.
	Linked map[string]jira.Issue

	// StoryLinkTypes are the link types that tie a container to the work
	// beneath it. Jira's parent field is taken by the Epic here, so the
	// association lives in issue links and differs between teams.
	StoryLinkTypes []string

	// PreviousClose is when the sprint before this one was actually
	// closed. Jira leaves a few minutes between one sprint being
	// completed and the next being started, and a ticket that finished in
	// that gap belongs to the sprint about to open rather than to nobody.
	PreviousClose time.Time

	// HoursPerPoint turns logged time into points, which is how a sprint
	// gets credit for work on a ticket that finished somewhere else.
	HoursPerPoint float64

	// WorklogAttribution says who a logged entry is credited to: its
	// author (config.AttributeToAuthor, the default) or the one person
	// its comment @mentions (config.AttributeToMention). Jira cannot log
	// time on somebody's behalf, so a team whose lead logs everyone's
	// carryover at sprint close names the real person in the comment,
	// and this is what lets the report read that.
	WorklogAttribution string

	// HoursPerDay is a working day in hours, for reading a breakdown
	// written in days beside a name in a worklog comment.
	HoursPerDay float64

	// AbsenceCost is how a day off is priced: config.AbsenceCostsPoint
	// takes a whole point, config.AbsenceCostsShare takes the baseline's
	// share of one sprint day.
	AbsenceCost string

	// EpicClasses maps a class name to a pattern matched against the epic
	// summary, so a team can split Run from Build however they label it.
	EpicClasses map[string]*regexp.Regexp

	// SprintLengthDays is the nominal sprint length in working days. It
	// no longer divides anything - a day off costs one point, because a
	// point is a day - but the panel states it so somebody reading a
	// baseline of ten knows what a full sprint is meant to look like.
	SprintLengthDays int

	// CapacityReviewedAt is when a person last confirmed this sprint's
	// availability, zero if nobody has. It is what separates a sprint
	// where everyone was available from one nobody has filled in.
	CapacityReviewedAt time.Time

	// Declared is what the team's Jira configuration said when this was
	// built, for the assumption panel to compare against what the
	// report did with it.
	Declared Declared
}

// window is the stretch of time this sprint gets to claim work in.
//
// It opens at the previous sprint's close where that is known, because
// the few minutes between completing one sprint and starting the next are
// not a hole work can fall into. It shuts at this sprint's own close -
// the timestamp of the Complete Sprint click, not the nominal end date,
// which teams routinely overrun by hours or days. An open sprint shuts at
// now, so work done so far still shows.
func (in Inputs) window() (time.Time, time.Time) {
	opens := in.Sprint.StartDate.Time
	if !in.PreviousClose.IsZero() && in.PreviousClose.Before(opens) {
		opens = in.PreviousClose
	}
	return opens, in.Sprint.Closed()
}

// sprintDays is the nominal sprint length in working days, for pricing a
// day off as a share of the baseline.
func (in Inputs) sprintDays() int {
	if in.SprintLengthDays <= 0 {
		return 10
	}
	return in.SprintLengthDays
}

// secondsPerPoint is what one point is worth in logged time.
func (in Inputs) secondsPerPoint() float64 {
	if in.HoursPerPoint <= 0 {
		return 6 * 3600
	}
	return in.HoursPerPoint * 3600
}

// Build computes the report. It reads nothing and writes nothing.
func Build(in Inputs) Report {
	r := Report{
		Sprint:      sprintInfo(in),
		GeneratedAt: time.Now().UTC(),
		// Empty slices rather than nil. This crosses to a browser as JSON,
		// and null where an array was promised took the whole page down
		// the first time a sprint had nothing concluded in it.
		Stories:     []Story{},
		Flags:       []Flag{},
		Assumptions: []Flag{},
		Carry:       []Row{},
	}
	r.Summary.DoneByStatus = map[string]int{}
	r.Summary.IssuesByType = map[string]int{}

	people := indexPeople(in.People)
	// Who the roster actually knows about, taken before any assignee
	// discovered in the issues is folded in below. Without it the two are
	// indistinguishable afterwards, and "not on the roster" is exactly the
	// distinction the report has to draw.
	rostered := make(map[string]bool, len(people))
	for id := range people {
		rostered[id] = true
	}
	capacity := indexCapacity(in.Capacity)
	opens, closes := in.window()
	work := linkedWork(in)

	// Accumulators.
	delivered := map[string]float64{}     // account id -> points
	rowsFor := map[string][]Row{}         // account id -> their issues
	epicPoints := map[string]*EpicGroup{} // epic key, or name -> group
	var unattributed float64
	var unlogged []Row
	// The work this sprint finished, kept for the estimate-against-actual
	// comparison. Only what concluded here: carryover has no actual yet,
	// and work that finished earlier was compared by the sprint that
	// finished it.
	var finished []Row

	for _, is := range in.Issues {
		life := newTimeline(is, in.Changes[is.ID], in.StatusCategories, in.Rules.Done)
		row := toRow(is, in, life, opens, closes)
		r.Summary.IssuesByType[row.Type]++

		// Containers roll up the work beneath them. Their points are a
		// total of their children, so they are never delivery - counting
		// them beside the tasks they roll up would count the same work
		// twice. They are reported in their own section instead.
		if in.Rules.IsContainer(row.Type) {
			items := work[is.Key]
			r.Summary.Containers++
			if len(items) == 0 {
				r.Summary.ContainersUnlinked++
			}
			st, here := concludedStory(row, items, in, opens, closes)
			if here {
				r.Stories = append(r.Stories, st)
			}
			// Flagged where it can still be acted on: the sprint that
			// concluded it, or any sprint it is still open in. A Story
			// that finished two sprints ago would otherwise raise the
			// same entry on every report it ever appeared in, which is
			// how a list of fixable things becomes wallpaper.
			if here || !row.Done || !allDone(items) {
				r.Flags = append(r.Flags, storyFlags(row, items, in)...)
			}
			continue
		}

		r.Summary.IssueCount++

		switch row.State {
		case StateFinishedEarlier:
			// Finished before this sprint opened and still on its board.
			// Neither delivered here nor carried out of here.
			r.Summary.FinishedEarlier++

		case StateCarried:
			r.Carry = append(r.Carry, row)
			if row.Active {
				r.Summary.CarriedActive++
				if !row.TimeLogged {
					unlogged = append(unlogged, row)
				}
			} else {
				r.Summary.NeverStarted++
			}

		case StateConcluded:
			r.Summary.DoneCount++
			r.Summary.DoneByStatus[row.Status]++
			finished = append(finished, row)
			if row.StartedEarlier {
				r.Summary.FinishedFromEarlier++
			}
			if (!row.HasPoints || row.UsedEstimate) && in.Rules.ExpectsPointsWhenDone(row.Type) {
				r.Summary.DoneUnsized++
			}
			if row.UsedEstimate {
				r.Flags = append(r.Flags, estimateFallbackFlag(row))
			}
			if !row.HasPoints && !row.TimeLogged && in.Rules.ExpectsPointsWhenDone(row.Type) {
				r.Flags = append(r.Flags, noSizeFlag(row, in))
			}
			if in.Rules.InSayDo(row.Type) {
				r.Summary.Completed++
			}
		}

		// Credit is what this sprint counts for the ticket, which is not
		// the same as the ticket's size: a ticket finishing here gives
		// back the points earlier sprints were already credited for it,
		// and one still open earns only what was worked on here.
		if row.Credited == 0 {
			continue
		}
		r.Summary.DeliveredTotal += row.Credited
		r.Summary.PriorPointsDeducted += row.PriorPoints
		addToEpic(epicPoints, row)

		credited := creditByPerson(is, row, opens, closes, in)
		for _, s := range credited.Split {
			r.Flags = append(r.Flags, splitFlag(row, s, in))
		}
		for _, d := range credited.Disagree {
			r.Flags = append(r.Flags, disagreeFlag(row, d, in))
		}
		if len(credited.Shares) == 0 {
			unattributed += row.Credited
			r.Flags = append(r.Flags, unassignedFlag(row, in))
			continue
		}
		for id, pts := range credited.Shares {
			delivered[id] += pts
			rowsFor[id] = append(rowsFor[id], row)
			if _, known := people[id]; !known {
				people[id] = store.Person{AccountID: id, Name: nameFor(is, id)}
			}
		}
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

	if f, ok := unloggedFlag(unlogged, in); ok {
		r.Flags = append(r.Flags, f)
	}

	r.Summary.DeliveredTotal = round2(r.Summary.DeliveredTotal)
	r.Summary.PriorPointsDeducted = round2(r.Summary.PriorPointsDeducted)
	r.Summary.UnattributedPoints = round2(unattributed)
	r.Summary.CarriedOver = len(r.Carry)
	r.Calibration = calibrate(finished, in)
	r.People = buildPeople(people, rostered, delivered, rowsFor, capacity, in, &r)
	r.CapacityReview = reviewState(in, r.People)
	r.Epics = finishEpics(epicPoints, r.Summary.DeliveredTotal, in)

	for _, p := range r.People {
		r.Summary.BaselineTotal += p.Baseline
		r.Summary.CapacityTotal += p.Capacity
		r.Summary.PlannedDaysOff += p.PlannedDaysOff
		r.Summary.UnplannedDaysOff += p.UnplannedDaysOff
		r.Summary.ShortfallFromAbsence += p.ShortfallFromAbsence
	}

	sortFlags(r.Flags)
	sort.SliceStable(r.Carry, func(i, j int) bool { return r.Carry[i].Key < r.Carry[j].Key })
	sort.SliceStable(r.Stories, func(i, j int) bool { return r.Stories[i].Points > r.Stories[j].Points })
	r.Assumptions = Assumptions(r, in)
	return r
}

func sprintInfo(in Inputs) SprintInfo {
	opens, closes := in.window()
	return SprintInfo{
		JiraID:      in.Sprint.ID,
		Number:      in.Sprint.Number,
		Name:        in.Sprint.Name,
		State:       in.Sprint.State,
		Starts:      in.Sprint.StartDate.Time,
		Ends:        in.Sprint.EndDate.Time,
		CountsFrom:  opens,
		CountsUntil: closes,
		Provisional: in.Sprint.Provisional,
		BoardID:     in.Sprint.OriginBoardID,
		BrowseURL: fmt.Sprintf("%s/jira/software/c/projects/%s/boards/%d/reports/sprint-retrospective?sprint=%d",
			in.BaseURL, in.Project, in.Sprint.OriginBoardID, in.Sprint.ID),
	}
}

// size is what a ticket cost, and where that figure came from.
//
// The fallback is for delivery only. Everywhere a report is asking what
// somebody actually recorded - a container against the work beneath it,
// most of all - the actual field alone is the answer, because the whole
// question there is whether the numbers people wrote down agree.
func size(is jira.Issue, in Inputs) (points float64, has bool, usedEstimate bool) {
	if pts, ok := is.Number(in.PointsField); ok {
		return pts, true, false
	}
	if in.EstimateField != "" && in.EstimateField != in.PointsField {
		if pts, ok := is.Number(in.EstimateField); ok {
			return pts, true, true
		}
	}
	return 0, false, false
}

// declared is what somebody actually wrote in the points field, with no
// fallback behind it.
func declared(is jira.Issue, in Inputs) (points float64, has bool, usedEstimate bool) {
	pts, ok := is.Number(in.PointsField)
	return pts, ok, false
}

func toRow(is jira.Issue, in Inputs, life timeline, opens, closes time.Time) Row {
	pts, has, usedEstimate := size(is, in)
	if in.Rules.IsContainer(is.Fields.IssueType.Name) {
		// A container is never delivery, so it never needs an estimate
		// standing in for a missing actual. What it recorded is the
		// point: a rollup read off the estimate field would be compared
		// against the work underneath and disagree for no reason.
		pts, has, usedEstimate = declared(is, in)
	}
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
	// The estimate is carried whether or not anything reads it here, so
	// the comparison against the actual can be made from rows alone. Only
	// when the two fields are genuinely different: one field doing both
	// jobs would have the ticket agreeing with itself.
	if in.EstimateField != "" && in.EstimateField != in.PointsField {
		row.Estimate, row.HasEstimate = is.Number(in.EstimateField)
	}

	row.ConcludedAt = concluded(is, in, life)
	row.Active = life.workedOnDuring(opens, closes)
	row.StartedEarlier = life.startedBefore(opens)

	inside, before, total := loggedSeconds(is, opens, closes)
	row.TimeLogged = total > 0 || is.Fields.TimeSpent > 0
	row.HoursLogged = round2(float64(inside) / 3600)

	switch {
	case row.ConcludedAt.IsZero(), row.ConcludedAt.After(closes):
		row.State = StateCarried
	case row.ConcludedAt.Before(opens):
		row.State = StateFinishedEarlier
	default:
		row.State = StateConcluded
		row.UsedEstimate = usedEstimate
	}
	if in.Rules.IsContainer(row.Type) {
		// A container is never credited, so none of the arithmetic below
		// applies to it. Its points are a rollup of work that is counted
		// on its own account.
		return row
	}

	row.Credited, row.PriorPoints = credit(is, in, life, row, inside, before, total)
	return row
}

// credit works out what this sprint counts for one ticket.
//
// A ticket's points belong to the work, not to a sprint, so a ticket that
// spans several has to be divided between them - and the divisions have to
// add up to the ticket and no more, or the same work gets paid for twice.
// Two ways of dividing, because the team has two kinds of ticket:
//
//   - With time logged against it, the log is a direct record of where the
//     work went, and the split follows it. A ticket finishing here hands
//     back what was logged before this sprint opened, since the sprint
//     that time was logged in has already been credited for it. One still
//     open earns what was logged here and nothing else.
//   - With no time logged anywhere - still most of them, the habit is new -
//     there is nothing to weigh the sprints by, so they share it equally.
//     Equally between the sprints it was worked on in, not the sprints it
//     sat on the board of: Jira's sprint field never forgets, and a sprint
//     where nobody touched a ticket did none of it.
func credit(is jira.Issue, in Inputs, life timeline, row Row, inside, before, total int) (credited, prior float64) {
	if row.State == StateFinishedEarlier {
		return 0, 0
	}

	if total > 0 {
		if row.State == StateCarried {
			return round2(float64(inside) / in.secondsPerPoint()), 0
		}
		if !row.HasPoints {
			return 0, 0
		}
		prior = round2(float64(before) / in.secondsPerPoint())
		credited = round2(row.Points - prior)
		if credited < 0 {
			credited = 0
		}
		return credited, prior
	}

	if !row.HasPoints {
		return 0, 0
	}
	// Nothing logged anywhere, so there is nothing to weigh the sprints
	// against each other by and they share the ticket equally. A sprint
	// that never picked it up gets no share: Jira's sprint field never
	// forgets, so presence on a board is not evidence that anything
	// happened. The sprint it finished in always takes a share, because
	// finishing it is evidence enough.
	if row.State == StateCarried && !row.Active {
		return 0, 0
	}
	n, known := activeSprintCount(is, in, life)
	if !known && row.State == StateCarried {
		// Nothing says how many sprints shared this ticket, so there is
		// no share to take. Crediting it here in full is the double count
		// this whole mechanism exists to stop: the sprint that finishes
		// it would be credited for it again.
		return 0, 0
	}
	share := round2(row.Points / float64(n))
	if row.State != StateConcluded {
		return share, 0
	}
	return share, round2(row.Points - share)
}

// activeSprintCount is how many of the sprints an issue has been on the
// board of it was actually worked on in.
//
// It is not known when the sprint field was not fetched or is empty,
// which the caller has to handle rather than assume one: assuming one
// would hand a single sprint the whole of a ticket several shared.
func activeSprintCount(is jira.Issue, in Inputs, life timeline) (n int, known bool) {
	for _, sp := range is.SprintsOn(in.SprintField) {
		if sp.StartDate.IsZero() {
			continue
		}
		known = true
		if life.workedOnDuring(sp.StartDate.Time, sp.Closed()) {
			n++
		}
	}
	if n < 1 {
		return 1, known
	}
	return n, known
}

// concluded is when the ticket reached a Done status for the last time.
//
// The changelog is the source, because a resolution date is set only by
// some workflows: a team whose first done status is a release gate gets
// no resolution date at all for a third of its finished work. The
// resolution date is the fallback for the rare issue whose history says
// nothing, and the creation date after that, so an issue that is plainly
// finished is never treated as though it never was.
func concluded(is jira.Issue, in Inputs, life timeline) time.Time {
	if !life.finished() {
		return time.Time{}
	}
	if at := life.concludedAt(); !at.IsZero() {
		return at
	}
	if !is.Fields.Resolved.IsZero() {
		return is.Fields.Resolved.Time
	}
	if !is.Fields.Updated.IsZero() {
		return is.Fields.Updated.Time
	}
	return is.Fields.Created.Time
}

// loggedSeconds splits an issue's worklog into time logged inside this
// window, time logged before it opened, and everything ever logged.
//
// Time logged after the window belongs to a later sprint and is that
// sprint's business. Time logged before is what an earlier sprint has
// already been credited for, and is what stops a ticket being paid for
// twice when it finally finishes.
func loggedSeconds(is jira.Issue, opens, closes time.Time) (inside, before, total int) {
	for _, w := range is.Fields.Worklog.Entries {
		at := w.Started.Time
		total += w.Seconds
		switch {
		case at.Before(opens):
			before += w.Seconds
		case at.After(closes):
		default:
			inside += w.Seconds
		}
	}
	return inside, before, total
}

// attribution is how one row's points divide between people.
type attribution struct {
	// Shares is the points each account is credited with.
	Shares map[string]float64

	// Split lists the entries that named several people without a figure
	// beside each name, so their time was divided equally. A fallback,
	// and flagged as one.
	Split []equalSplit

	// Disagree lists the entries whose written figures add up to
	// something other than the time logged. The entry was divided by the
	// figures regardless; which number is right is for a person.
	Disagree []disagreement
}

// disagreement is one logged entry whose figures do not add up to it.
type disagreement struct {
	Hours float64 // Time Spent
	Sum   float64 // the figures beside the names, added
	Unit  string  // their unit, or empty when none was written
}

// equalSplit is one logged entry divided equally between the people it
// names, for want of a breakdown.
type equalSplit struct {
	Hours   float64
	People  int
	Partial bool // a figure beside some names but not all
}

// creditByPerson splits a row's credit across whoever logged time on it
// during the sprint, falling back to the assignee.
//
// Assignee alone is wrong for anything that spans sprints: a ticket sits
// with whoever it was handed to last, which is frequently not who did the
// work. Where time has been logged, that is a direct record of who did
// what and it wins. Where none has, the assignee is the right answer
// rather than a compromise - most tickets have no log, and that is fine.
//
// Who an entry records as having done the work depends on the team.
// Under author attribution it is whoever logged it. Under mention
// attribution it is the people the comment @mentions: Jira has no way to
// log time on another person's behalf, so a lead closing out a sprint
// names them there instead. One name takes the whole entry. Several
// names divide it by the figure written beside each - "@A 3h @B 1h" -
// and, where there is no figure, equally, which is reported so it can be
// corrected. An entry naming nobody, or only its author, is the author's.
func creditByPerson(is jira.Issue, row Row, opens, closes time.Time, in Inputs) attribution {
	if row.Credited == 0 {
		return attribution{}
	}
	byPerson := map[string]float64{}
	total := 0.0
	var split []equalSplit
	var disagree []disagreement
	for _, w := range is.Fields.Worklog.Entries {
		at := w.Started.Time
		if at.Before(opens) || at.After(closes) || w.Author == nil || w.Author.AccountID == "" {
			continue
		}
		secs := float64(w.Seconds)
		total += secs

		if in.WorklogAttribution != config.AttributeToMention {
			byPerson[w.Author.AccountID] += secs
			continue
		}
		named := w.MentionShares()
		switch {
		case len(named) == 0:
			byPerson[w.Author.AccountID] += secs
		case len(named) == 1:
			byPerson[named[0].ID] += secs
		default:
			weights, partial := shareWeights(named, in)
			if weights == nil {
				split = append(split, equalSplit{Hours: secs / 3600, People: len(named), Partial: partial})
				for _, m := range named {
					byPerson[m.ID] += secs / float64(len(named))
				}
				continue
			}
			sum := 0.0
			for _, wt := range weights {
				sum += wt
			}
			for i, m := range named {
				byPerson[m.ID] += secs * weights[i] / sum
			}
			if d, off := figuresDisagree(named, secs/3600, in); off {
				disagree = append(disagree, d)
			}
		}
	}
	if total == 0 {
		if is.Fields.Assignee == nil || is.Fields.Assignee.AccountID == "" {
			return attribution{}
		}
		return attribution{Shares: map[string]float64{is.Fields.Assignee.AccountID: row.Credited}}
	}

	out := attribution{Shares: make(map[string]float64, len(byPerson)), Split: split, Disagree: disagree}
	for id, secs := range byPerson {
		if share := round2(row.Credited * secs / total); share != 0 {
			out.Shares[id] = share
		}
	}
	return out
}

// shareWeights turns the figures written beside each name into
// comparable weights, in hours. It returns nil when any name lacks a
// figure - a partial breakdown is not one - and then says whether some
// names did carry one, so the fallback can be described accurately.
//
// A unit is converted only when the figures disagree about theirs. A
// team writing "3,5" and "1" with no unit means the same unit both
// times, whatever it is, and the ratio is all that matters.
func shareWeights(named []jira.Mention, in Inputs) (weights []float64, partial bool) {
	units := map[string]bool{}
	with := 0
	for _, m := range named {
		if m.HasFigure {
			with++
		}
		units[m.Unit] = true
	}
	if with < len(named) {
		return nil, with > 0
	}
	weights = make([]float64, len(named))
	for i, m := range named {
		weights[i] = m.Figure
		if len(units) > 1 {
			switch m.Unit {
			case "d":
				weights[i] *= in.hoursPerDay()
			case "sp":
				weights[i] *= in.secondsPerPoint() / 3600
			}
		}
	}
	return weights, false
}

// figuresDisagree says whether the figures beside the names add up to the
// time logged, within a tenth. With a unit there is one reading. Without
// one the figures are tried as hours, as days and as points, and only an
// entry that matches none of them is reported: "3,5" and "1" against 27
// hours is a team writing points, and needs no remark.
//
// A disagreement is not resolved here. The entry was already divided by
// the figures, because a ratio survives the confusion; which total is
// right does not, and a person has to say.
func figuresDisagree(named []jira.Mention, hours float64, in Inputs) (disagreement, bool) {
	sum := 0.0
	units := map[string]bool{}
	for _, m := range named {
		sum += m.Figure
		units[m.Unit] = true
	}
	if len(units) != 1 || sum <= 0 || hours <= 0 {
		return disagreement{}, false // mixed units were converted to hours already; nothing to compare
	}
	var unit string
	for u := range units {
		unit = u
	}
	readings := map[string]float64{"h": 1, "d": in.hoursPerDay(), "sp": in.secondsPerPoint() / 3600}
	try := []string{unit}
	if unit == "" {
		try = []string{"h", "d", "sp"}
	}
	for _, u := range try {
		if as := sum * readings[u]; math.Abs(as-hours) <= hours*0.1 {
			return disagreement{}, false
		}
	}
	return disagreement{Hours: hours, Sum: sum, Unit: unit}, true
}

// disagreeFlag names a logged entry whose figures do not add up to it.
func disagreeFlag(row Row, d disagreement, in Inputs) Flag {
	hours := strconv.FormatFloat(d.Hours, 'f', -1, 64)
	sum := strconv.FormatFloat(d.Sum, 'f', -1, 64)
	var written string
	switch d.Unit {
	case "":
		written = fmt.Sprintf("%s with no unit, which is %sh as points or days", sum,
			strconv.FormatFloat(d.Sum*in.secondsPerPoint()/3600, 'f', -1, 64))
	case "h":
		written = sum + "h"
	case "d":
		written = fmt.Sprintf("%s days, which is %sh", sum, strconv.FormatFloat(d.Sum*in.hoursPerDay(), 'f', -1, 64))
	default:
		written = fmt.Sprintf("%s points, which is %sh", sum, strconv.FormatFloat(d.Sum*in.secondsPerPoint()/3600, 'f', -1, 64))
	}
	return Flag{
		Kind: FlagWorklogFiguresDisagree,
		Key:  row.Key,
		Message: fmt.Sprintf("%s has %sh logged, but the figures beside the names add up to %s. The entry was "+
			"divided by the figures; one of the two numbers is wrong, and Jira reports the logged one",
			row.Key, hours, written),
		URL: row.URL,
	}
}

// hoursPerDay is a working day in hours, for reading a figure written in
// days beside a name.
func (in Inputs) hoursPerDay() float64 {
	if in.HoursPerDay <= 0 {
		return 6
	}
	return in.HoursPerDay
}

// splitFlag names a logged entry that was divided equally between the
// people it names, because nothing said how it should divide. Equal is a
// guess, so it is shown; the fix is a figure beside each name, or one
// entry per person.
func splitFlag(row Row, s equalSplit, in Inputs) Flag {
	hours := strconv.FormatFloat(s.Hours, 'f', -1, 64)
	why := "with no breakdown"
	if s.Partial {
		why = "with a figure beside only some of the names"
	}
	return Flag{
		Kind: FlagWorklogSplitEqually,
		Key:  row.Key,
		Message: fmt.Sprintf("%s has %sh logged in one entry naming %d people %s, so it is divided equally "+
			"between them. Write the hours beside each name (@Person 3h) to divide it by effort, or log "+
			"one entry per person", row.Key, hours, s.People, why),
		URL: row.URL,
	}
}

// nameFor finds a display name for an account id seen on this issue.
func nameFor(is jira.Issue, accountID string) string {
	if is.Fields.Assignee != nil && is.Fields.Assignee.AccountID == accountID {
		return is.Fields.Assignee.DisplayName
	}
	for _, w := range is.Fields.Worklog.Entries {
		if w.Author != nil && w.Author.AccountID == accountID {
			return w.Author.DisplayName
		}
	}
	return accountID
}

// round2 keeps derived points to two decimals. Logged time divided by a
// rate produces long fractions that mean nothing, and a total assembled
// from them reads as false precision.
func round2(f float64) float64 { return math.Round(f*100) / 100 }

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

// buildPeople turns the roster and the sprint's assignees into rows.
//
// Three standings, not two, and the difference is what the opt-in model
// buys. Somebody on the roster and opted in is measured against their
// baseline. Somebody on the roster and opted out - a manager, someone on
// loan to another team - keeps their history and is simply not measured.
// Somebody not on the roster at all delivered work here without being
// ours to plan for, and their points still count towards the sprint.
//
// Only the first contributes to the capacity totals, because a total that
// mixed measured and unmeasured people would be a number with no meaning:
// the delivery of eight people against the capacity of five.
func buildPeople(
	people map[string]store.Person,
	rostered map[string]bool,
	delivered map[string]float64,
	rowsFor map[string][]Row,
	capacity map[string]store.Capacity,
	in Inputs,
	r *Report,
) []Person {
	out := make([]Person, 0, len(people))
	for id, p := range people {
		cap := capacity[id]
		onRoster := rostered[id]
		optedIn := onRoster && p.Active
		person := Person{
			AccountID: id,
			Name:      p.Name,
			Delivered: round2(delivered[id]),
			Note:      cap.Note,
			OnRoster:  onRoster,
			Measured:  optedIn && p.Baseline > 0,
			Rows:      rowsFor[id],
		}
		if person.Rows == nil {
			person.Rows = []Row{}
		}

		// Somebody opted out who delivered nothing this sprint is not part
		// of this sprint. Their row would be six dashes and a name.
		if onRoster && !optedIn && person.Delivered == 0 && len(person.Rows) == 0 {
			continue
		}

		if person.Measured {
			person.Baseline = p.Baseline
			person.PlannedDaysOff = cap.PlannedDaysOff
			person.UnplannedDaysOff = cap.UnplannedDaysOff

			// Capacity is the baseline less what PLANNED leave costs, and
			// planned only.
			//
			// By default a day off costs one point whatever the baseline:
			// a point is a day, that is the convention the baseline is
			// expressed in, so a day away is a point not delivered. Pricing
			// it as a share of the baseline instead would make absence
			// cheaper for somebody with a smaller one - a person on six
			// losing three days would come out at 4.2 rather than 3 - which
			// reads as though being part-time makes a day off cost less.
			//
			// That argument assumes the baseline is a full-time figure. For
			// somebody whose three points are spread thinly across the
			// whole sprint - a lead, a part-timer on the team every day - a
			// day away really does cost 0.3 of them, and taking a whole
			// point would say one day off cost a third of their sprint. So
			// the share rule exists, and a team chooses it knowingly.
			//
			// Unplanned absence is deliberately not subtracted. Capacity
			// is what the team committed to knowing what it knew, and it
			// did not know about the illness. Taking it off here would
			// erase the miss it caused: the shortfall would vanish into a
			// smaller capacity and the sprint would read as though it
			// went to plan. It is reported below instead, as part of what
			// a shortfall is made of, priced the same way.
			dayCost := 1.0
			if in.AbsenceCost == config.AbsenceCostsShare {
				dayCost = p.Baseline / float64(in.sprintDays())
			}
			person.Capacity = round2(person.Baseline - cap.PlannedDaysOff*dayCost)
			if person.Capacity < 0 {
				person.Capacity = 0
			}
			person.Delta = round2(person.Delivered - person.Capacity)

			if person.Delta < 0 && cap.UnplannedDaysOff > 0 {
				person.ShortfallFromAbsence = math.Min(round2(cap.UnplannedDaysOff*dayCost), -person.Delta)
			}
		}

		// Somebody nobody has ever registered. Worth raising once: either
		// they belong on the roster, or they are another team's and the
		// answer is to leave them off, which the opt-out records.
		if !onRoster && person.Delivered > 0 {
			r.Flags = append(r.Flags, Flag{
				Kind: FlagDeliveredOffRoster,
				Message: fmt.Sprintf("%s delivered %.1f points and is not on the roster. Those points still count "+
					"towards the sprint; add them under Settings to measure them against a baseline, or leave them off",
					person.Name, person.Delivered),
			})
		}
		// Opted in with nothing to be measured against is a half-finished
		// setup rather than a deliberate answer, so it is named.
		if optedIn && p.Baseline <= 0 {
			r.Flags = append(r.Flags, Flag{
				Kind: FlagNoBaseline,
				Message: fmt.Sprintf("%s is on the roster with no baseline, so their delivery is measured against nothing",
					person.Name),
			})
		}
		// Only worth saying while nobody has confirmed the sprint. Once
		// somebody has been through it, naming the rows they did not
		// change is second-guessing an answer they have already given.
		if in.CapacityReviewedAt.IsZero() && person.Measured && !cap.Reviewed {
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

// reviewState reports which of the three states this sprint's capacity is
// in. Only the timestamp is stored; whether anything was adjusted is read
// off the rows, so the two can never disagree.
//
// It counts the people the report measures rather than the stored
// capacity rows, because those are not the same set. A row survives
// somebody being opted out, so counting rows would have the badge say
// "three away" over a table showing two - and the badge is the summary of
// that table, not of the database.
func reviewState(in Inputs, people []Person) CapacityReview {
	out := CapacityReview{State: ReviewNone}
	for _, p := range people {
		if p.Measured && p.PlannedDaysOff+p.UnplannedDaysOff > 0 {
			out.Adjusted++
		}
	}
	if in.CapacityReviewedAt.IsZero() {
		return out
	}
	at := in.CapacityReviewedAt
	out.ReviewedAt = &at
	out.State = ReviewNoAdjustments
	if out.Adjusted > 0 {
		out.State = ReviewAdjusted
	}
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
	g.Points += row.Credited
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
		g.Points = round2(g.Points)
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

func noSizeFlag(row Row, in Inputs) Flag {
	return Flag{
		Kind: FlagDoneNoEstimate,
		Key:  row.Key,
		Message: fmt.Sprintf("%s (%s) finished with no points, no estimate and no time logged, so there is nothing to size it by",
			row.Key, row.Type),
		URL: row.URL,
		JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND issuetype != %s AND %s IS EMPTY AND status IN (%s)`,
			in.Sprint.ID, firstOr(in.Rules.Container, "Epic"), jqlField(in.PointsField), quoteList(in.Rules.Done))),
	}
}

func estimateFallbackFlag(row Row) Flag {
	return Flag{
		Kind: FlagEstimateFallback,
		Key:  row.Key,
		Message: fmt.Sprintf("%s finished without its actual points, so its estimate of %.1f was counted instead",
			row.Key, row.Points),
		URL: row.URL,
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
	if row.Credited > 0 {
		return fmt.Sprintf("%s has nobody assigned and no time logged, so its %.1f points are credited to no one",
			row.Key, row.Credited)
	}
	return fmt.Sprintf("%s finished with nobody assigned", row.Key)
}

// unloggedFlag gathers the carried-over work nobody logged time against.
//
// One entry, not one per ticket. Measured across four closed sprints this
// catches between four and nine tickets each time, and nine rows would
// bury the rest of a list meant to be read to the end. The count and the
// names are what somebody acts on; the tickets are carried behind them.
//
// Only for a closed sprint, and only for work somebody actually picked
// up. While a sprint is running nobody is late logging anything yet, and
// a ticket that sat untouched has nothing to log.
func unloggedFlag(rows []Row, in Inputs) (Flag, bool) {
	if len(rows) == 0 || !strings.EqualFold(in.Sprint.State, "closed") {
		return Flag{}, false
	}

	counts := map[string]int{}
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		who := r.Assignee
		if who == "" {
			who = "nobody assigned"
		}
		counts[who]++
		keys = append(keys, r.Key)
	}
	sort.Strings(keys)

	names := make([]string, 0, len(counts))
	for who := range counts {
		names = append(names, who)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, who := range names {
		parts = append(parts, fmt.Sprintf("%s (%d)", who, counts[who]))
	}

	subject := fmt.Sprintf("%d carried-over tickets were", len(rows))
	if len(rows) == 1 {
		subject = "1 carried-over ticket was"
	}
	return Flag{
		Kind: FlagCarriedNoWorklog,
		Message: fmt.Sprintf("%s worked on with no time logged against them: %s. Logging it is what lets "+
			"the next sprint hand this one credit for the work it did", subject, strings.Join(parts, ", ")),
		Keys: keys,
		JQL: jqlLink(in, fmt.Sprintf(`sprint = %d AND timespent IS EMPTY AND status NOT IN (%s)`,
			in.Sprint.ID, quoteList(in.Rules.Done))),
	}, true
}

// jqlLink builds a Jira search URL, so a number can be checked rather
// than believed.
func jqlLink(in Inputs, jql string) string {
	return in.BaseURL + "/issues/?jql=" + urlEscape(jql)
}

// jqlField turns a REST field id into something JQL will accept.
//
// The API answers to customfield_10033 and so does the REST search, but
// the issue navigator refuses it - "Field 'customfield_10033' does not
// exist or you do not have permission to view it" - which is how a flag's
// link arrived broken. cf[10033] is the form JQL is documented to take,
// and unlike the field's name it cannot be ambiguous: a site can have
// both "Story Points" and "Story point estimate", as some do.
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

func firstOr(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// sortFlags puts the kinds that need action before the informational ones.
func sortFlags(flags []Flag) {
	rank := map[string]int{
		FlagDoneUnassigned:         0,
		FlagDoneNoEstimate:         1,
		FlagStoryNothingSized:      2,
		FlagStoryPointsMismatch:    3,
		FlagStoryNoWork:            4,
		FlagStoryWorkDoneNotShut:   5,
		FlagEstimateFallback:       6,
		FlagWorklogSplitEqually:    7,
		FlagWorklogFiguresDisagree: 8,
		FlagNoBaseline:             9,
		FlagDeliveredOffRoster:     10,
		FlagCarriedNoWorklog:       11,
		FlagCapacityUnreviewed:     12,
	}
	sort.SliceStable(flags, func(i, j int) bool {
		if rank[flags[i].Kind] != rank[flags[j].Kind] {
			return rank[flags[i].Kind] < rank[flags[j].Kind]
		}
		return flags[i].Key < flags[j].Key
	})
}
