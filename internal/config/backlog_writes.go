package config

import "strings"

// Which end of a story link the child sits at.
const (
	// LinkChildInward puts the child in inwardIssue and the Story in
	// outwardIssue. The default.
	LinkChildInward = "inward"

	// LinkChildOutward is the other way round.
	LinkChildOutward = "outward"
)

// BacklogStoryLinkChild says which end of a story link the child sits
// at when the backlog tool links a ticket to a Story.
//
// Direction is a team habit rather than a standard. A "Blocks" link
// reads "PROJECT-1 blocks PROJECT-2" from one side and "is blocked by"
// from the other, and which side the team puts the Story on was decided
// the first time somebody made one by hand. The report reads links from
// either side, so nothing breaks when this is wrong - but a link made
// the other way round from the rest looks wrong on the ticket, and
// people notice. Anything unrecognised reads as inward.
func BacklogStoryLinkChild() string {
	if strings.ToLower(String("ARGUS_BACKLOG_STORY_LINK_CHILD", LinkChildInward)) == LinkChildOutward {
		return LinkChildOutward
	}
	return LinkChildInward
}
