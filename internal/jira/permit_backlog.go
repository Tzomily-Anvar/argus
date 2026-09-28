package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// The Backlog tool's side of the permitted list: labels, the parent, one
// link made or removed, issues moved onto a sprint or back to the
// backlog, and the one write nothing can undo, deleting an issue. Kept
// apart from the field edits because these are bulk writes - a preview
// of fifty tickets rather than one - and because the one entry that
// needs a second setting lives here.
//
// Every body is exact: the keys named and no other. A key the list does
// not name is a field somebody's team did not agree to have edited, and
// the way to add one is to add it here, in review.

var (
	issueLinkPath      = regexp.MustCompile(`^/rest/api/3/issueLink$`)
	issueLinkEntryPath = regexp.MustCompile(`^/rest/api/3/issueLink/[0-9]+$`)
	sprintIssuesPath   = regexp.MustCompile(`^/rest/agile/1.0/sprint/[0-9]+/issue$`)
	backlogIssuesPath  = regexp.MustCompile(`^/rest/agile/1.0/backlog/issue$`)

	// issueKey is the shape a key has wherever a body names one, the
	// same shape the issue path insists on. Numeric ids are refused for
	// the same reason there: the preview shows keys, and the request
	// should name the same thing the person approved.
	issueKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)
)

// notifyQuery is the query every bulk field edit carries. A batch that
// emails the team once per ticket is how a tool gets banned.
const notifyQuery = "notifyUsers=false"

// deleteQuery is what an issue delete carries. Subtasks go with their
// parent, because Jira refuses to delete a parent that still has any and
// the preview has already shown the person what is going.
const deleteQuery = "deleteSubtasks=true"

// maxSprintMove is the most issues one sprint move names. Jira's own
// limit on the endpoint is fifty; a larger batch is sent in pages.
const maxSprintMove = 50

// labelsBody accepts {"update": {"labels": [{"add": "x"}, {"remove": "y"},
// ...]}}: at least one operation, each exactly one of add or remove with
// a non-empty label, and nothing else at any level. It is the "update"
// form of the edit rather than the "fields" form on purpose: setting
// the whole list would silently drop a label somebody added between
// the preview and the write.
func labelsBody(raw []byte, _ writePolicy) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if len(top) != 1 || top["update"] == nil {
		return errors.New(`the body must be exactly {"update": {"labels": [...]}}`)
	}
	var update map[string]json.RawMessage
	if err := json.Unmarshal(top["update"], &update); err != nil || len(update) != 1 || update["labels"] == nil {
		return errors.New("update must carry exactly one key, labels")
	}
	var ops []map[string]json.RawMessage
	if err := json.Unmarshal(update["labels"], &ops); err != nil || len(ops) == 0 {
		return errors.New("labels must be a non-empty array of operations")
	}
	for _, op := range ops {
		if len(op) != 1 {
			return errors.New("each label operation is exactly one of add or remove")
		}
		for verb, label := range op {
			if verb != "add" && verb != "remove" {
				return fmt.Errorf("%s is not a label operation this tool makes", verb)
			}
			if err := nonEmptyString(label); err != nil {
				return fmt.Errorf("the label to %s %v", verb, err)
			}
		}
	}
	return nil
}

// parentBody accepts {"fields": {"parent": {"key": "<issue key>"}}}, or null in
// place of the parent, and nothing else. Null is the reversal of a set
// on an issue that had no parent, as it is for points and the assignee.
func parentBody(raw []byte, _ writePolicy) error {
	name, value, err := singleField(raw)
	if err != nil {
		return err
	}
	if name != "parent" {
		return fmt.Errorf("%s is not the parent field", name)
	}
	if isNull(value) {
		return nil
	}
	var parent map[string]json.RawMessage
	if err := json.Unmarshal(value, &parent); err != nil || len(parent) != 1 || parent["key"] == nil {
		return errors.New(`parent must be exactly {"key": "..."}`)
	}
	return keyString(parent["key"], "parent key")
}

// linkCreateBody accepts {"type": {"name": "..."}, "inwardIssue": {"key":
// ...}, "outwardIssue": {"key": ...}} with the two keys different. No
// comment on the link: that is a second write dressed as a field.
func linkCreateBody(raw []byte, _ writePolicy) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if err := exactKeys(top, "type", "inwardIssue", "outwardIssue"); err != nil {
		return err
	}
	var typ map[string]json.RawMessage
	if err := json.Unmarshal(top["type"], &typ); err != nil || len(typ) != 1 || typ["name"] == nil {
		return errors.New(`type must be exactly {"name": "..."}`)
	}
	if err := nonEmptyString(typ["name"]); err != nil {
		return fmt.Errorf("the link type name %v", err)
	}
	keys := map[string]string{}
	for _, side := range []string{"inwardIssue", "outwardIssue"} {
		var ref map[string]json.RawMessage
		if err := json.Unmarshal(top[side], &ref); err != nil || len(ref) != 1 || ref["key"] == nil {
			return fmt.Errorf(`%s must be exactly {"key": "..."}`, side)
		}
		if err := keyString(ref["key"], side+" key"); err != nil {
			return err
		}
		var k string
		_ = json.Unmarshal(ref["key"], &k)
		keys[side] = k
	}
	if keys["inwardIssue"] == keys["outwardIssue"] {
		return errors.New("a link needs two different issues")
	}
	return nil
}

// issuesBody accepts {"issues": ["<issue key>", ...]}: one to fifty keys and
// nothing else. The same shape serves a move onto a sprint and a move
// back to the backlog.
func issuesBody(raw []byte, _ writePolicy) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if len(top) != 1 || top["issues"] == nil {
		return errors.New(`the body must be exactly {"issues": [...]}`)
	}
	var keys []json.RawMessage
	if err := json.Unmarshal(top["issues"], &keys); err != nil {
		return errors.New("issues must be an array of keys")
	}
	if len(keys) == 0 || len(keys) > maxSprintMove {
		return fmt.Errorf("issues must name between 1 and %d keys, not %d", maxSprintMove, len(keys))
	}
	for _, k := range keys {
		if err := keyString(k, "an issue"); err != nil {
			return err
		}
	}
	return nil
}

// keyString requires raw to be a string in the shape of an issue key.
func keyString(raw json.RawMessage, what string) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || !issueKey.MatchString(s) {
		return fmt.Errorf("%s must be an issue key, PROJECT-123", what)
	}
	return nil
}
