package config

import "strings"

// The one required setting, read forgivingly.
//
// ARGUS_GITHUB_ORG is the name in github.com/<name>. What people put
// there, when they set it by hand, is the browser's address bar: that is
// where someone looks when asked which account they mean. The first
// teammate to install Argus did exactly that, and got an empty dashboard
// with nothing to say why. So the name is taken out of whatever was
// written, wherever the setting is read, and anything that is still not
// a name is refused with the rule spelled out.

// AccountName pulls the account name out of what was typed or pasted: a
// bare name, a profile address, an organisation page, or any of those
// with a query string behind it. The second result is false when no
// GitHub login can be read from it.
func AccountName(answer string) (string, bool) {
	s := strings.TrimSpace(answer)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")

	for _, part := range strings.Split(s, "/") {
		switch {
		case part == "":
			continue
		case strings.Contains(part, "."):
			continue // the host of a pasted address
		case part == "orgs" || part == "enterprises":
			continue // what github.com puts in front of the name
		}
		if !ValidLogin(part) {
			return "", false
		}
		return part, true
	}
	return "", false
}

// ValidLogin follows GitHub's own rule for a login: letters, digits and
// hyphens, none at either end, and no more than 39 characters. It is the
// same rule for an organisation and for a person.
func ValidLogin(s string) bool {
	if s == "" || len(s) > 39 || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// OrgHint is the one sentence every refusal of the setting ends with.
const OrgHint = "It is the name in github.com/<name> - an organisation or your own username - " +
	"letters, digits and hyphens. Pasting the page's address works too."
