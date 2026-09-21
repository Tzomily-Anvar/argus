package sprint

import "net/url"

func urlEscape(s string) string { return url.QueryEscape(s) }
