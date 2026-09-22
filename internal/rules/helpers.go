package rules

import "github.com/Tzomily-Anvar/argus/internal/gh"

// Small local aliases so rule files read cleanly without importing gh
// just for a string cast.

func gstr(v any) string { return gh.Str(v) }

// strs guards a nil slice so it encodes as [] rather than null.
func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// A rate limit must not look like an answer.
//
// Every rule here fans out over pull requests or repositories and treats
// a failed item as one to leave out. That is right for a repository with
// a feature switched off, and wrong for a rate limit: the rule then
// returns a shorter list that reads exactly like a complete one, and "no
// stale branches" is a very different claim from "Argus ran out of
// budget before it could look".
//
// So a rate limit fails the whole rule, loudly. The dashboard shows a
// rule's error where its rows would be, which is the honest rendering of
// a sweep that could not finish.

// checked is one fanned-out result: whatever was produced, and why it
// might be incomplete.
type checked struct {
	row map[string]any
	err error
}

// unpack splits fanned-out results into rows, unless one of them failed
// for a reason that invalidates the lot.
func unpack(got []checked) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(got))
	for _, g := range got {
		if gh.IsRateLimited(g.err) {
			return nil, g.err
		}
		rows = append(rows, g.row)
	}
	return rows, nil
}

// firstRateLimit returns the first rate-limit failure among many, or nil.
func firstRateLimit(errs []error) error {
	for _, err := range errs {
		if gh.IsRateLimited(err) {
			return err
		}
	}
	return nil
}
