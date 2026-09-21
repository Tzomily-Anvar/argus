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
