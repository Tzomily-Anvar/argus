package publish

import "strings"

// diffLines is a line-by-line difference between two blocks: the lines
// they share, the lines only the old one has, and the lines only the new
// one has, in the order a reader expects. A longest common subsequence
// over the lines - a few hundred at most, so the plain quadratic table
// is fine - and no dependency, because the one thing this has to be is
// legible.
func diffLines(before, after string) []DiffLine {
	a, b := lines(before), lines(after)
	out := make([]DiffLine, 0, len(a)+len(b))
	if len(a)*len(b) > 4_000_000 {
		// Two blocks too large to compare line by line are not something
		// a person will read line by line either: everything old goes,
		// everything new comes.
		for _, l := range a {
			out = append(out, DiffLine{Kind: "del", Text: l})
		}
		for _, l := range b {
			out = append(out, DiffLine{Kind: "add", Text: l})
		}
		return out
	}

	// table[i][j] is the length of the longest common subsequence of
	// a[i:] and b[j:], filled from the end so the walk below reads
	// forwards and emits lines in their natural order.
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Kind: "same", Text: a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			out = append(out, DiffLine{Kind: "del", Text: a[i]})
			i++
		default:
			out = append(out, DiffLine{Kind: "add", Text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, DiffLine{Kind: "del", Text: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, DiffLine{Kind: "add", Text: b[j]})
	}
	return out
}

// lines splits a block, treating an empty block as no lines rather than
// one empty line.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
