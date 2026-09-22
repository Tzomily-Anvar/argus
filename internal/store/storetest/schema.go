package storetest

// The shape of what is on disk is part of the contract, not an
// implementation detail.
//
// Argus stores things nobody can reconstruct: the roster, each person's
// baseline, who was away in a sprint two years ago, and the statement
// that a sprint's availability was checked. None of it has a source to
// re-fetch from. A schema change that loses it is therefore not a bug to
// fix in the next release - the data is gone.
//
// Adding a field is safe: records written before it existed read as its
// zero value. These are not, and all of them fail silently rather than
// loudly:
//
//   - renaming a field: the old value is ignored and the new one reads as
//     zero, which is indistinguishable from "never set"
//   - retyping a field: JSON decoding fails, or worse, coerces
//   - repurposing a field: old values survive and now mean something else
//
// CheckJSONFields is what makes those a decision rather than an accident.
// It reflects over a persisted type and compares the exact set of JSON
// names and types against a checked-in expectation, so any change to the
// shape fails the build with the reason attached.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// CheckJSONFields asserts that v's persisted JSON shape is exactly want:
// a map of JSON field name to the Go type written under it, spelled as
// Go spells it ("string", "float64", "*time.Time", "map[string]float64").
//
// The type is recorded, not merely the kind, because a kind is too coarse
// to notice a retype that matters: time.Time and any other struct are
// both "struct", and a timestamp silently becoming something else is
// precisely the class of change this exists to catch.
func CheckJSONFields(t *testing.T, name string, v any, want map[string]string) {
	t.Helper()

	got := jsonFields(reflect.TypeOf(v))

	var added, removed, retyped []string
	for field, typ := range got {
		wantType, ok := want[field]
		switch {
		case !ok:
			added = append(added, field+" "+typ)
		case wantType != typ:
			retyped = append(retyped, fmt.Sprintf("%s: was %s, now %s", field, wantType, typ))
		}
	}
	for field, typ := range want {
		if _, ok := got[field]; !ok {
			removed = append(removed, field+" "+typ)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(retyped)

	if len(added) == 0 && len(removed) == 0 && len(retyped) == 0 {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "the stored shape of %s has changed.\n", name)

	if len(removed) > 0 {
		b.WriteString("\nGONE from the struct, still on disk in every existing install:\n")
		for _, f := range removed {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString(
			"\nA field that disappears - whether removed or renamed - is a silent\n" +
				"data loss. Existing files still carry the old name; the new struct\n" +
				"ignores it, and the value reads as its zero value, which nothing can\n" +
				"tell apart from \"never set\". There is no source to re-fetch a\n" +
				"baseline or an absence from.\n" +
				"\nInstead:\n" +
				"  - to rename: add the new field, keep reading the old one for one\n" +
				"    release, and write both; drop the old one only after that\n" +
				"  - to retire: leave the field in place and stop using it, so old\n" +
				"    files still decode\n")
	}

	if len(retyped) > 0 {
		b.WriteString("\nRETYPED, so existing files no longer decode as they did:\n")
		for _, f := range retyped {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString(
			"\nEvery file already written holds the old type. Decoding either fails\n" +
				"outright or coerces quietly. Add a differently named field of the new\n" +
				"type and convert on read for one release instead.\n")
	}

	if len(added) > 0 {
		b.WriteString("\nADDED:\n")
		for _, f := range added {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString(
			"\nAdding a field is safe - older records read it as its zero value - so\n" +
				"this is not a refusal. Add it to the expectation in this test to say\n" +
				"the shape changed on purpose, and check the zero value is a sensible\n" +
				"reading of a record written before the field existed.\n")
	}

	b.WriteString("\nWhatever the change, add a case to the golden-fixture test proving\n" +
		"a directory written by the previous release still reads correctly.\n")

	t.Error(b.String())
}

// jsonFields returns the JSON name and Go type of every field
// encoding/json would write for a struct type.
func jsonFields(t reflect.Type) map[string]string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported, never persisted
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name // encoding/json falls back to the Go name
		}
		out[name] = f.Type.String()
	}
	return out
}
