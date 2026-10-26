package backlog

import (
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

// Label spelling: how a team keeps one label from becoming three.
//
// Jira treats labels as case-sensitive strings and creates one on first
// use, so a label drifts the moment two people type it: argus-api,
// argusApi and Argus_API are three labels, and a filter on one misses
// the other two. Everything here rests on one observation - those three
// are the same words with different case and separators - so a label's
// key is its letters and digits in lower case, and two labels with one
// key are one label spelt two ways.
//
// The spelling a team settles on is a setting, never inferred from the
// labels in use: the labels in use are what drifted. What is in use is
// only ever reported, as the tally the Labels view shows to help choose.

// LabelWords splits a label into its words: at any character that is not
// a letter or digit, where a lower-case letter meets an upper-case one,
// and before the last capital of a run of them followed by lower case,
// so that APIGateway is API and Gateway. Digits stay with the word they
// follow.
func LabelWords(label string) []string {
	var words []string
	rs := []rune(label)
	start := -1
	flush := func(end int) {
		if start >= 0 && end > start {
			words = append(words, string(rs[start:end]))
		}
		start = -1
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		prev := rs[i-1]
		switch {
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush(i)
			start = i
		case unicode.IsUpper(r) && unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
			flush(i)
			start = i
		}
	}
	flush(len(rs))
	return words
}

// LabelKey is what two spellings of one label share: its letters and
// digits, in lower case.
func LabelKey(label string) string {
	var b strings.Builder
	for _, r := range label {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// SpellLabel writes a label in a style. Any leaves it as it is. Train
// capitalises each word and keeps the capitals inside it, so that an
// acronym stays one (Auth-API); the others put every word in lower
// case first. The result always has the label's key, so respelling
// never turns one label into a different one.
func SpellLabel(label, style string) string {
	words := LabelWords(label)
	if style == config.LabelStyleAny || len(words) == 0 {
		return label
	}
	for i, w := range words {
		if style != config.LabelStyleTrain {
			w = strings.ToLower(w)
		}
		if style == config.LabelStylePascal || style == config.LabelStyleTrain || (style == config.LabelStyleCamel && i > 0) {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		words[i] = w
	}
	switch style {
	case config.LabelStyleKebab, config.LabelStyleTrain:
		return strings.Join(words, "-")
	case config.LabelStyleSnake:
		return strings.Join(words, "_")
	}
	return strings.Join(words, "")
}

// LabelFits says whether a label is already spelt in a style.
func LabelFits(label, style string) bool { return SpellLabel(label, style) == label }

// SpellingFinding is one label to tidy: the labels to migrate from, and
// the one spelling to migrate them to.
type SpellingFinding struct {
	// Target is the spelling to keep. It may be one of the labels in use
	// or, for a label spelt against the style, one nobody has used yet.
	Target string `json:"target"`
	// Variants are the labels to replace, never including Target.
	Variants []LabelCount `json:"variants"`
	// Reason is "spellings" when one label is spelt more than one way,
	// "style" when a label is spelt one way, but not the team's.
	Reason string `json:"reason"`
}

// LabelSpelling lists the labels in use to tidy, the most tickets first.
//
// Exempt labels are the ones a setting names, such as the request label,
// and the ones the team chose behind the gear: renaming one would
// quietly break the view that looks for it, or undo a choice somebody
// made on purpose, so an exempt label is never itself flagged for its
// style, and where it is one of several spellings, it is the one to
// keep. Among the rest, a spelling that is exactly one of repos is kept
// before a more used one, for a team that names labels after its
// repositories.
func LabelSpelling(inUse []LabelCount, style string, exempt, repos []string) []SpellingFinding {
	byKey := map[string][]LabelCount{}
	var keys []string
	for _, l := range inUse {
		k := LabelKey(l.Name)
		if k == "" {
			continue
		}
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], l)
	}
	isExempt := func(name string) int {
		for i, e := range exempt {
			if e == name {
				return i
			}
		}
		return -1
	}

	var out []SpellingFinding
	for _, k := range keys {
		group := byKey[k]
		// Most used first, then by name, so the choices below are stable.
		sort.Slice(group, func(i, j int) bool {
			if group[i].Count != group[j].Count {
				return group[i].Count > group[j].Count
			}
			return group[i].Name < group[j].Name
		})

		if len(group) == 1 {
			l := group[0]
			if style == config.LabelStyleAny || isExempt(l.Name) >= 0 || LabelFits(l.Name, style) {
				continue
			}
			out = append(out, SpellingFinding{Target: SpellLabel(l.Name, style), Variants: group, Reason: "style"})
			continue
		}

		target := ""
		best := len(exempt)
		for _, l := range group {
			if i := isExempt(l.Name); i >= 0 && i < best {
				target, best = l.Name, i
			}
		}
		if target == "" {
			// The spellings that may be kept: those in the style, or all
			// of them when there is none; a repository's own name first,
			// then the most used.
			var fit []LabelCount
			for _, l := range group {
				if style == config.LabelStyleAny || LabelFits(l.Name, style) {
					fit = append(fit, l)
				}
			}
			for _, l := range fit {
				if slices.Contains(repos, l.Name) {
					target = l.Name
					break
				}
			}
			switch {
			case target != "":
			case len(fit) > 0:
				target = fit[0].Name
			default:
				target = SpellLabel(group[0].Name, style)
			}
		}
		variants := make([]LabelCount, 0, len(group))
		for _, l := range group {
			if l.Name != target {
				variants = append(variants, l)
			}
		}
		out = append(out, SpellingFinding{Target: target, Variants: variants, Reason: "spellings"})
	}

	total := func(f SpellingFinding) int {
		n := 0
		for _, v := range f.Variants {
			n += v.Count
		}
		return n
	}
	sort.SliceStable(out, func(i, j int) bool { return total(out[i]) > total(out[j]) })
	return out
}

// StyleTally is how the labels in use of more than one word are spelt,
// for choosing a style. A label can fit only one style once it has two
// words, since the styles differ in their separators and capitals.
type StyleTally struct {
	Kebab  int `json:"kebab"`
	Snake  int `json:"snake"`
	Camel  int `json:"camel"`
	Pascal int `json:"pascal"`
	Train  int `json:"train"`
	// Other counts labels of several words spelt none of the four ways,
	// such as Argus_API.
	Other int `json:"other"`
}

// TallyLabelStyles counts the labels in use by style, one each however
// many tickets carry them. A one-word label fits several styles at once
// and says nothing about the team's habit, so it is left out.
func TallyLabelStyles(inUse []LabelCount) StyleTally {
	var t StyleTally
	for _, l := range inUse {
		if len(LabelWords(l.Name)) < 2 {
			continue
		}
		switch {
		case LabelFits(l.Name, config.LabelStyleTrain):
			t.Train++
		case LabelFits(l.Name, config.LabelStyleKebab):
			t.Kebab++
		case LabelFits(l.Name, config.LabelStyleSnake):
			t.Snake++
		case LabelFits(l.Name, config.LabelStyleCamel):
			t.Camel++
		case LabelFits(l.Name, config.LabelStylePascal):
			t.Pascal++
		default:
			t.Other++
		}
	}
	return t
}

// RepoLabel is a repository offered as a label.
type RepoLabel struct {
	Repo string `json:"repo"`
	// Label is how to write it: a label that already exists with the
	// repository's key, so picking it never makes a second spelling, or
	// the repository's name in the team's style.
	Label string `json:"label"`
	// Count is how many open tickets carry Label.
	Count int `json:"count"`
}

// ActiveRepos is the repositories pushed to within the last days, by
// name. Zero days is none.
func ActiveRepos(repos []sweep.Repo, days int, now time.Time) []string {
	out := []string{}
	if days <= 0 {
		return out
	}
	cut := now.Add(-time.Duration(days) * 24 * time.Hour)
	for _, r := range repos {
		if !r.PushedAt.Before(cut) {
			out = append(out, r.Name)
		}
	}
	return out
}

// RepoLabels offers each of the active repositories as a label.
func RepoLabels(repos []string, inUse []LabelCount, site []string, style string) []RepoLabel {
	counts := map[string]int{}
	for _, l := range inUse {
		counts[l.Name] = l.Count
	}
	// The existing spelling to use for a key: one in use before one that
	// is only on the site, the team's own spelling before another, and
	// the most used before the rest. inUse comes most used first.
	existing := func(key, want string) string {
		found := ""
		for _, l := range inUse {
			if LabelKey(l.Name) == key {
				if l.Name == want {
					return want
				}
				if found == "" {
					found = l.Name
				}
			}
		}
		if found != "" {
			return found
		}
		for _, l := range site {
			if LabelKey(l) == key {
				if l == want {
					return want
				}
				if found == "" {
					found = l
				}
			}
		}
		return found
	}

	seen := map[string]bool{}
	out := []RepoLabel{}
	for _, r := range repos {
		want := SpellLabel(r, style)
		label := existing(LabelKey(r), want)
		if label == "" {
			label = want
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, RepoLabel{Repo: r, Label: label, Count: counts[label]})
	}
	sort.Slice(out, func(i, j int) bool { return alphabetical(out[i].Label, out[j].Label) })
	return out
}

// alphabetical orders labels ignoring case, and by case where that is
// all that tells them apart, so the order never depends on the sort.
func alphabetical(a, b string) bool {
	if la, lb := strings.ToLower(a), strings.ToLower(b); la != lb {
		return la < lb
	}
	return a < b
}

// LabelsElsewhere is the site's labels the open backlog does not carry,
// in alphabetical order.
func LabelsElsewhere(site []string, inUse []LabelCount) []string {
	have := make(map[string]bool, len(inUse))
	for _, l := range inUse {
		have[l.Name] = true
	}
	out := []string{}
	for _, l := range site {
		if !have[l] {
			have[l] = true
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return alphabetical(out[i], out[j]) })
	return out
}
