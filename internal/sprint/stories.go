package sprint

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// workItem is one Task or Bug sitting underneath a container.
type workItem struct {
	Key       string
	Type      string
	Status    string
	Points    float64
	HasPoints bool
	Done      bool

	// ConcludedAt is when it reached a Done status, zero while it has
	// not. A container finishes when the last of these does, so a missing
	// one is what keeps a Story in flight.
	ConcludedAt time.Time
}

// linkedWork resolves each container's links into the work beneath it.
//
// Jira's parent field points at the Epic for this team, so the Story to
// Task association has nowhere to live but issue links. Which link types
// carry it is configuration, because teams use different ones and some
// carry a legacy type from a migration alongside the current one.
//
// Links to Epics and to other containers are dropped: an Epic above a
// Story is not work underneath it, and counting it would make a Story's
// children include its own parent.
func linkedWork(in Inputs) map[string][]workItem {
	if len(in.StoryLinkTypes) == 0 {
		return nil
	}
	out := map[string][]workItem{}
	for _, is := range in.Issues {
		if !in.Rules.IsContainer(is.Fields.IssueType.Name) {
			continue
		}
		var items []workItem
		seen := map[string]bool{}
		for _, link := range is.Fields.Links {
			if !has(in.StoryLinkTypes, link.Type.Name) {
				continue
			}
			other := link.Other()
			if other == nil || seen[other.Key] {
				continue
			}
			if !in.Rules.IsWork(other.Fields.IssueType.Name) {
				continue
			}
			seen[other.Key] = true
			items = append(items, in.workItem(other))
		}
		sort.SliceStable(items, func(a, b int) bool { return items[a].Key < items[b].Key })
		out[is.Key] = items
	}
	return out
}

// workItem fills in what a linked issue's own record says. The stub Jira
// embeds in a link carries a status and a type but no custom fields, so
// points and history come from the full issue where one was fetched.
func (in Inputs) workItem(stub *jira.LinkedIssue) workItem {
	item := workItem{
		Key:    stub.Key,
		Type:   stub.Fields.IssueType.Name,
		Status: stub.Fields.Status.Name,
		Done:   in.Rules.IsDone(stub.Fields.Status.Name),
	}
	full, ok := in.Linked[stub.Key]
	if !ok {
		return item
	}
	// The actual field alone. Comparing a container against its work is a
	// question about what people recorded, so an estimate standing in for
	// a missing actual would answer a different one.
	item.Points, item.HasPoints, _ = declared(full, in)
	life := newTimeline(full, in.Changes[full.ID], in.StatusCategories, in.Rules.Done)
	item.Done = life.finished()
	item.ConcludedAt = concluded(full, in, life)
	return item
}

// pointsUnderneath adds up the work beneath a container, and says how much
// of it anybody sized.
func pointsUnderneath(items []workItem) (total float64, sized int) {
	for _, it := range items {
		if it.HasPoints {
			sized++
			total += it.Points
		}
	}
	return round2(total), sized
}

// allDone reports whether every item underneath has finished.
func allDone(items []workItem) bool {
	for _, it := range items {
		if !it.Done {
			return false
		}
	}
	return true
}

// concludedStory decides whether a container finished in this sprint, and
// when.
//
// A Story is finished when the Story is done AND everything linked
// beneath it is done. A Story moved to a Done status over work still in
// flight has not finished - somebody shut the parent early - and the
// sprint that closes the last of that work is the one that gets to claim
// it. So the moment it concluded is the latest of its own transition and
// its children's, never its own alone.
//
// A Story with nothing linked to it cannot fail that test, having no
// children to be waiting on, and is counted as concluded on its own
// transition. It carries a flag saying nothing was defined underneath it,
// which is a statement about the work rather than about this tool.
func concludedStory(row Row, items []workItem, in Inputs, opens, closes time.Time) (Story, bool) {
	if !row.Done || !allDone(items) || row.ConcludedAt.IsZero() {
		return Story{}, false
	}

	at := row.ConcludedAt
	for _, it := range items {
		if it.ConcludedAt.After(at) {
			at = it.ConcludedAt
		}
	}
	if at.Before(opens) || at.After(closes) {
		return Story{}, false
	}

	linked, _ := pointsUnderneath(items)
	return Story{
		Key:          row.Key,
		Summary:      row.Summary,
		Points:       row.Points,
		Epic:         row.Epic,
		URL:          row.URL,
		LinkedPoints: linked,
		LinkedCount:  len(items),
		ConcludedAt:  at,
	}, true
}

// pointsTolerance is how far a rollup may sit from the work underneath
// before it is worth saying anything. Points are halved and thirded here,
// so exact equality would fire on arithmetic rather than on a mistake.
const pointsTolerance = 0.05

// storyFlags compares a container against the work beneath it.
//
// This replaced a flag that fired on every Story carrying points at all,
// saying the points were a rollup. That is true, normal, and correct data
// for this team, so it fired on everything and named nothing anybody
// could fix. What is worth saying is where the rollup and the work
// underneath disagree, because one of the two is then wrong.
func storyFlags(row Row, items []workItem, in Inputs) []Flag {
	var flags []Flag

	// Finished with nothing linked underneath. It is not that the tool
	// cannot find the work: there is no work recorded, so nobody can say
	// what was done. Never summed as zero, which would quietly report the
	// Story as agreeing with its children.
	if row.Done && len(items) == 0 {
		return append(flags, Flag{
			Kind: FlagStoryNoWork,
			Key:  row.Key,
			Message: fmt.Sprintf("%s finished with no defined work: nothing is linked underneath it, "+
				"so there is no record of what was actually done", row.Key),
			URL: row.URL,
		})
	}

	// Everything underneath is finished but the Story is still open. The
	// work is done and nobody shut it, which is a minute's tidying rather
	// than a mystery - and until somebody does, the sprint that did the
	// work cannot claim it.
	if !row.Done && len(items) > 0 && allDone(items) {
		flags = append(flags, Flag{
			Kind: FlagStoryWorkDoneNotShut,
			Key:  row.Key,
			Message: fmt.Sprintf("%s is still %s although all %d linked items are finished, "+
				"so no sprint can count it", row.Key, row.Status, len(items)),
			URL: row.URL,
		})
	}

	// Beyond here the comparison needs a claim to compare against. A
	// container with no points of its own is not claiming anything: this
	// team fills the rollup in on some Stories and not others, and
	// flagging the blanks would be back to firing on normal data.
	if !row.HasPoints || row.Points <= 0 || len(items) == 0 {
		return flags
	}

	linked, sized := pointsUnderneath(items)
	switch {
	case sized == 0:
		flags = append(flags, Flag{
			Kind: FlagStoryNothingSized,
			Key:  row.Key,
			Message: fmt.Sprintf("%s claims %.1f points but nothing underneath is estimated: %d linked items, none sized",
				row.Key, row.Points, len(items)),
			URL: row.URL,
		})
	case math.Abs(linked-row.Points) > pointsTolerance:
		flags = append(flags, Flag{
			Kind: FlagStoryPointsMismatch,
			Key:  row.Key,
			Message: fmt.Sprintf("%s carries %.1f points but the work underneath adds up to %.1f across %d linked items",
				row.Key, row.Points, linked, len(items)),
			URL: row.URL,
		})
	}
	return flags
}
