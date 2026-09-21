package gh

// Helpers for walking GitHub's decoded JSON. Every one of these is
// nil-safe and returns a usable zero value, so a shape that differs from
// what we expect degrades to "field missing" instead of panicking.

func Map(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func List(v any) []any {
	l, _ := v.([]any)
	return l
}

func Str(v any) string {
	s, _ := v.(string)
	return s
}

func Num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func Bool(v any) bool {
	b, _ := v.(bool)
	return b
}

// Maps filters a decoded JSON array down to its object elements.
func Maps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if m, ok := raw.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
