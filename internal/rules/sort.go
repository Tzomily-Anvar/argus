package rules

import "sort"

func sortStable(rows []map[string]any, less func(a, b map[string]any) bool) {
	sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
}
