package jira

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The worklog side of the permitted list: the three operations, the
// query they must carry, and the body rules they share. Kept apart from
// the field edits because they are a different kind of write - an entry
// is created or removed rather than a value set - and because the
// one-entry-per-person invariant lives here and nowhere else.

// worklogFields are the only keys a worklog write may carry, each with
// the check its value must pass. Anything else - visibility, author,
// issueId - is a key nobody reviewed. Ordered, so a refusal names the
// same missing field every time.
var worklogFields = []struct {
	name  string
	check func(json.RawMessage) error
}{
	{"started", nonEmptyString},
	{"timeSpentSeconds", positiveNumber},
	{"comment", oneMention},
}

// worklogBody checks the fields a worklog add or update carries. An add
// needs all three; an update may correct any of them, but has to change
// something.
func worklogBody(raw []byte, all bool) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if len(top) == 0 {
		return errors.New("the worklog entry sets nothing")
	}
	known := map[string]bool{}
	for _, f := range worklogFields {
		known[f.name] = true
		value, present := top[f.name]
		if !present && all {
			return fmt.Errorf("%s is required", f.name)
		}
		if present {
			if err := f.check(value); err != nil {
				return fmt.Errorf("%s %v", f.name, err)
			}
		}
	}
	for k := range top {
		if !known[k] {
			return fmt.Errorf("%s is not a field this tool writes on a worklog entry", k)
		}
	}
	return nil
}

func worklogAddBody(raw []byte, _ writePolicy) error    { return worklogBody(raw, true) }
func worklogUpdateBody(raw []byte, _ writePolicy) error { return worklogBody(raw, false) }

// positiveNumber is what Time Spent has to be. Zero logs nothing and
// negative is not time; either is a form that lost its value on the way.
func positiveNumber(raw json.RawMessage) error {
	if n, ok := number(raw); !ok || n <= 0 {
		return errors.New("must be a positive number")
	}
	return nil
}

// oneMention requires the comment to be an Atlassian Document Format
// document that mentions exactly one person. This is the one-entry-per-
// person invariant, enforced where a caller cannot forget it: an entry
// naming two people against a single Time Spent cannot be read back
// without guessing which hours are whose, and an entry naming nobody
// credits whoever held the token. The text beside the mention is not
// read at all; it is the operator's note, not data.
func oneMention(raw json.RawMessage) error {
	var root adfNode
	if err := json.Unmarshal(raw, &root); err != nil || root.Type != "doc" {
		return errors.New("must be an Atlassian Document Format document")
	}
	if n := root.mentions(); n != 1 {
		return fmt.Errorf("must mention exactly one person, not %d", n)
	}
	return nil
}

// countMentions counts the mention nodes in an ADF document, wherever
// they sit in the tree. Anything that is not a document holds none.
func countMentions(raw json.RawMessage) int {
	var root adfNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return 0
	}
	return root.mentions()
}
