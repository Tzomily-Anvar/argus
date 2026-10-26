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

// The label spellings a team can settle on.
const (
	LabelStyleAny    = "any"
	LabelStyleKebab  = "kebab"
	LabelStyleSnake  = "snake"
	LabelStyleCamel  = "camel"
	LabelStylePascal = "pascal"
	LabelStyleTrain  = "train"
)

// LabelStyles is every spelling, in the order the settings list them.
var LabelStyles = []string{LabelStyleAny, LabelStyleKebab, LabelStyleSnake, LabelStyleCamel, LabelStylePascal, LabelStyleTrain}

// BacklogLabelStyle is how the team spells a label of more than one
// word: kebab-case, snake_case, camelCase, PascalCase or Train-Case. It
// is a choice the team states rather than one read off the labels it
// has, because the labels in use are exactly what drifted. Anything unrecognised
// reads as any, which checks nothing.
func BacklogLabelStyle() string {
	v := strings.ToLower(String("ARGUS_BACKLOG_LABEL_STYLE", LabelStyleAny))
	for _, s := range LabelStyles {
		if v == s {
			return s
		}
	}
	return LabelStyleAny
}

// BacklogRepoLabelDays is how recently a repository must have been
// pushed to for the label pickers to offer its name. Zero offers none.
func BacklogRepoLabelDays() int { return Int("ARGUS_BACKLOG_REPO_LABEL_DAYS", 90) }
