package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// The Confluence side of the permitted list: one page created, one page
// updated, and nothing else under /wiki/. Kept apart from the Jira
// entries because a page is a different kind of thing to write - a body
// of markup rather than a field - and because the one rule that makes a
// page safe to write again lives here: the markers.
//
// The client's base URL is the site root, so a Confluence path begins
// with /wiki/. Attachments, comments, labels, properties and the whole
// of /wiki/rest/ are not matched by either pattern and so are refused
// before the policy is consulted, whether writes are on or off.

// The markers Argus's block sits between. They are spelled again in
// internal/page, which owns the block; this package cannot import it
// without a cycle, so a test in internal/publish holds the two copies
// equal. Every page body this client sends must carry both, once, in
// order, so that Argus can always find its own block to replace and
// never sends a page it could not update again.
const (
	PageMarkerStart = `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">argus-report-start</ac:parameter></ac:structured-macro>`
	PageMarkerEnd   = `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">argus-report-end</ac:parameter></ac:structured-macro>`

	// Confluence rewrites a macro on save, so a marker on a page is
	// recognised by its anchor name, not its exact text. These are the
	// page package's patterns, held equal by the same test.
	PageMarkerStartPattern = `<ac:structured-macro\b[^>]*\bac:name="anchor"[^>]*>\s*<ac:parameter\s+ac:name="">\s*argus-report-start\s*</ac:parameter>\s*</ac:structured-macro>`
	PageMarkerEndPattern   = `<ac:structured-macro\b[^>]*\bac:name="anchor"[^>]*>\s*<ac:parameter\s+ac:name="">\s*argus-report-end\s*</ac:parameter>\s*</ac:structured-macro>`
)

var (
	pageMarkerStart = regexp.MustCompile(PageMarkerStartPattern)
	pageMarkerEnd   = regexp.MustCompile(PageMarkerEndPattern)
)

var (
	pagesPath = regexp.MustCompile(`^/wiki/api/v2/pages$`)
	pagePath  = regexp.MustCompile(`^/wiki/api/v2/pages/[0-9]+$`)
)

// pageCreateBody accepts exactly {spaceId, status, title, parentId, body}
// - the space and the parent as strings, because that is how the v2 API
// spells an id, the status pinned to "current" so a draft can never be
// created by accident, and a body that carries the markers.
func pageCreateBody(raw []byte, _ writePolicy) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if err := exactKeys(top, "spaceId", "status", "title", "parentId", "body"); err != nil {
		return err
	}
	for _, k := range []string{"spaceId", "title", "parentId"} {
		if err := nonEmptyString(top[k]); err != nil {
			return fmt.Errorf("%s %v", k, err)
		}
	}
	if err := isCurrent(top["status"]); err != nil {
		return err
	}
	return storageBody(top["body"])
}

// pageUpdateBody accepts exactly {id, status, title, body, version}. The
// id must be the one in the path: the two name the same page, and a body
// that disagrees with its path is a caller that has mixed two pages up.
// The version is Confluence's own conditional write - it refuses a
// number that is not current plus one - so it is required here and must
// be a whole number above zero.
func pageUpdateBody(raw []byte, path string, _ writePolicy) error {
	top, err := object(raw)
	if err != nil {
		return err
	}
	if err := exactKeys(top, "id", "status", "title", "body", "version"); err != nil {
		return err
	}
	var id string
	if err := json.Unmarshal(top["id"], &id); err != nil || id == "" {
		return errors.New("id must be a non-empty string")
	}
	if id != path[strings.LastIndex(path, "/")+1:] {
		return fmt.Errorf("id %q is not the page the path names", id)
	}
	if err := isCurrent(top["status"]); err != nil {
		return err
	}
	if err := nonEmptyString(top["title"]); err != nil {
		return fmt.Errorf("title %v", err)
	}
	if err := storageBody(top["body"]); err != nil {
		return err
	}
	return pageVersion(top["version"])
}

// exactKeys refuses a key the list does not name and a key it names that
// is missing. Named, so a refusal says which.
func exactKeys(obj map[string]json.RawMessage, keys ...string) error {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
		if _, ok := obj[k]; !ok {
			return fmt.Errorf("%s is required", k)
		}
	}
	for k := range obj {
		if !want[k] {
			return fmt.Errorf("%s is not a field this tool writes on a page", k)
		}
	}
	return nil
}

func isCurrent(raw json.RawMessage) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s != "current" {
		return errors.New(`status must be "current"`)
	}
	return nil
}

// storageBody accepts {representation: "storage", value: "..."} where the
// value carries the two markers, once each, start before end. Storage
// format alone, because it is the one representation Argus can read
// back and splice into; a page sent as wiki markup or ADF could not be
// updated by the same code.
func storageBody(raw json.RawMessage) error {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return errors.New("body is not a JSON object")
	}
	if err := exactKeys(body, "representation", "value"); err != nil {
		return fmt.Errorf("body: %v", err)
	}
	var rep string
	if err := json.Unmarshal(body["representation"], &rep); err != nil || rep != "storage" {
		return errors.New(`body.representation must be "storage"`)
	}
	var value string
	if err := json.Unmarshal(body["value"], &value); err != nil {
		return errors.New("body.value must be a string")
	}
	starts, ends := pageMarkerStart.FindAllStringIndex(value, -1), pageMarkerEnd.FindAllStringIndex(value, -1)
	if len(starts) != 1 || len(ends) != 1 {
		return fmt.Errorf("body.value must carry each marker exactly once, not %d and %d", len(starts), len(ends))
	}
	if ends[0][0] < starts[0][1] {
		return errors.New("body.value has its end marker before its start marker")
	}
	return nil
}

// pageVersion accepts {number: <positive whole number>} with an optional
// message, a string. Nothing else: minorEdit and the rest are choices
// nobody reviewed.
func pageVersion(raw json.RawMessage) error {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		return errors.New("version is not a JSON object")
	}
	num, ok := v["number"]
	if !ok {
		return errors.New("version.number is required")
	}
	n, isNum := number(num)
	if !isNum || n < 1 || n != math.Trunc(n) {
		return errors.New("version.number must be a whole number above zero")
	}
	for k, val := range v {
		switch k {
		case "number":
		case "message":
			var s string
			if err := json.Unmarshal(val, &s); err != nil {
				return errors.New("version.message must be a string")
			}
		default:
			return fmt.Errorf("version.%s is not a field this tool writes", k)
		}
	}
	return nil
}
