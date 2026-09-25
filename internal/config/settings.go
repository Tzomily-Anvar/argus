package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The catalogue of settings, so that something other than a person
// reading .env.example can answer "what can I change, and what is it now?"
//
// Every knob Argus reads is declared here once, with its type and its
// built-in default. That is what lets `argus config` reject a misspelt
// key instead of writing it to the file, reject "yes please" where a
// number belongs, and tell you which of the environment, the file, or the
// default your current value came from.
//
// This list and .env.example are kept in step by TestEverySettingIsDocumented,
// which reads every setting name out of the source and insists on finding
// it documented. Adding a setting here therefore means documenting it.
//
// Rules are not in here. They declare their own parameters and register
// themselves, so their settings are derived from the rule registry at the
// point of use - a rule added tomorrow is configurable tomorrow, with no
// list to remember to update.

// Kind is a setting's value type. Knowing it is what makes it possible to
// refuse a bad value at the moment it is typed rather than at startup,
// when whoever typed it has long since walked away.
type Kind string

const (
	KindString Kind = "text"
	KindInt    Kind = "whole number"
	KindFloat  Kind = "number"
	KindBool   Kind = "true or false"
	KindList   Kind = "comma-separated list"
	KindEnum   Kind = "one of a fixed set"
	KindURL    Kind = "URL"
	KindPath   Kind = "directory path"
	KindPort   Kind = "port number"
)

// Setting is one knob.
type Setting struct {
	// Key is the environment variable name, which is also the name used
	// in the configuration file. There is only ever one name for a thing.
	Key string

	// Section groups settings in `argus config list`, in the same order
	// and under the same headings as .env.example, so the two read alike.
	Section string

	// Desc is one line: what it does, not how it is spelt.
	Desc string

	Kind Kind

	// Values are the permitted values, for KindEnum.
	Values []string

	// Default is the built-in default, written as it would appear in the
	// file. Empty means the setting has no default at all.
	Default string

	// Secret marks a credential. Its value is masked wherever it is
	// printed, and writing one to the file is something Argus warns about
	// rather than does quietly.
	Secret bool

	// EnvOnly marks a setting that cannot usefully live in the
	// configuration file, because it is read before the file is found or
	// is set by the container image.
	EnvOnly bool

	// Rule names the rule a setting belongs to, empty for core settings.
	// Set by the caller that derives settings from the rule registry.
	Rule string
}

// Catalogue is a set of settings, in display order.
type Catalogue []Setting

// Core returns the settings the binary itself reads. Rule settings are
// added by the caller, which is the only place that may import the rule
// registry without a cycle.
func Core() Catalogue {
	return Catalogue{
		{
			Key: "ARGUS_GITHUB_ORG", Section: "Required", Kind: KindString,
			Desc: "The GitHub organisation, or your own username, to sweep. " +
				"The one setting with no default.",
		},

		{
			Key: "ARGUS_TOOLS", Section: "Which tools run", Kind: KindList, Default: "pr",
			Desc: "Tools to run: pr (pull request triage), sprint (sprint reports).",
		},

		{
			Key: "ARGUS_REPOS", Section: "Which repositories", Kind: KindList,
			Desc: "An allowlist of repository names. Unset sweeps the whole organisation.",
		},
		{
			Key: "ARGUS_EXCLUDE_REPOS", Section: "Which repositories", Kind: KindList,
			Desc: "Repositories to leave out. Ignored when ARGUS_REPOS is set.",
		},
		{
			Key: "ARGUS_INCLUDE_ARCHIVED", Section: "Which repositories", Kind: KindBool,
			Default: "false",
			Desc:    "Sweep archived repositories too. Off, because an archive holds nothing actionable.",
		},

		{
			Key: "ARGUS_PORT", Section: "Server", Kind: KindPort, Default: "18474",
			Desc: "The port to listen on. http://argus.localhost:<port> works with no setup.",
		},
		{
			Key: "ARGUS_BIND", Section: "Server", Kind: KindString, Default: "127.0.0.1",
			Desc: "The interface to listen on. Loopback, because the dashboard has no authentication.",
		},
		{
			Key: "ARGUS_REFRESH_MINUTES", Section: "Server", Kind: KindInt, Default: "15",
			Desc: "How often the background sweep runs. Lower is fresher and costs more API quota.",
		},
		{
			Key: "ARGUS_CONCURRENCY", Section: "Server", Kind: KindInt, Default: "10",
			Desc: "Maximum GitHub requests in flight at once, across every rule.",
		},
		{
			Key: "ARGUS_HTTP_TIMEOUT_SECONDS", Section: "Server", Kind: KindInt, Default: "45",
			Desc: "Per-request timeout when talking to GitHub, in seconds.",
		},

		{
			Key: "ARGUS_GITHUB_TOKEN", Section: "Credentials", Kind: KindString, Secret: true,
			Desc: "A personal access token. `argus login` is the alternative, and needs no token.",
		},
		{
			Key: "ARGUS_GITHUB_APP_CLIENT_ID", Section: "Credentials", Kind: KindString,
			Default: publishedAppClientID,
			Desc:    "The GitHub App `argus login` signs in to. A client id is public, not a credential.",
		},
		{
			Key: "ARGUS_GITHUB_TOKEN_TYPE", Section: "Credentials", Kind: KindEnum,
			Values: []string{"auto", "fine-grained", "classic"}, Default: "auto",
			Desc: "Which kind of token you hold. Detected from its prefix; only the wording of errors changes.",
		},

		{
			Key: "ARGUS_JIRA_BASE_URL", Section: "Jira", Kind: KindURL,
			Desc: "Your Jira site, e.g. https://example.atlassian.net",
		},
		{
			Key: "ARGUS_JIRA_EMAIL", Section: "Jira", Kind: KindString,
			Desc: "The address you log in to Jira with. Jira Cloud wants it alongside the token.",
		},
		{
			Key: "ARGUS_JIRA_TOKEN", Section: "Jira", Kind: KindString, Secret: true,
			Desc: "A Jira API token, from id.atlassian.com/manage-profile/security/api-tokens",
		},
		{
			Key: "ARGUS_JIRA_PROJECT", Section: "Jira", Kind: KindString,
			Desc: "The project key whose sprints you report on.",
		},
		{
			Key: "ARGUS_JIRA_DONE_STATUSES", Section: "Jira", Kind: KindList, Default: "Done",
			Desc: "Status names that count as delivered. Matched by name, not by Jira's done category.",
		},
		{
			Key: "ARGUS_JIRA_EXCLUDED_TYPES", Section: "Jira", Kind: KindList, Default: "Epic",
			Desc: "Issue types left out of the say/do count.",
		},
		{
			Key: "ARGUS_JIRA_CONTAINER_TYPES", Section: "Jira", Kind: KindList, Default: "Story",
			Desc: "Issue types whose points roll up work beneath them, reported as work concluded.",
		},
		{
			Key: "ARGUS_JIRA_ESTIMATED_ON_RESOLVE", Section: "Jira", Kind: KindList, Default: "Bug",
			Desc: "Issue types estimated when the work finishes rather than when it is planned.",
		},
		{
			Key: "ARGUS_JIRA_EPIC_CLASSES", Section: "Jira", Kind: KindList,
			Default: `Run:^\[?run\]?,Build:^\[?build\]?`,
			Desc:    "How epics are grouped for the delivery split, as name:pattern pairs.",
		},
		{
			Key: "ARGUS_JIRA_SPRINT_LENGTH_DAYS", Section: "Jira", Kind: KindInt, Default: "10",
			Desc: "Nominal sprint length in working days, used to turn days off into a share of a baseline.",
		},
		{
			Key: "ARGUS_JIRA_POINTS_FIELD", Section: "Jira", Kind: KindString,
			Desc: "Pin the story points custom field id. Resolved by name when left unset.",
		},
		{
			Key: "ARGUS_JIRA_POINTS_FIELD_NAME", Section: "Jira", Kind: KindString,
			Default: "Story Points",
			Desc:    "The field name to resolve story points by.",
		},
		{
			Key: "ARGUS_JIRA_ESTIMATE_FIELD_NAME", Section: "Jira", Kind: KindString,
			Default: "Story point estimate",
			Desc:    "The field to fall back on when finished work never had its actual points filled in.",
		},
		{
			Key: "ARGUS_JIRA_STORY_LINK_TYPES", Section: "Jira", Kind: KindList,
			Default: "Blocks,migration_parent,Relates",
			Desc:    "Issue link types that tie a container to the work beneath it, when parent points at the Epic.",
		},
		{
			Key: "ARGUS_JIRA_WORKLOG_ATTRIBUTION", Section: "Jira", Kind: KindEnum,
			Values: []string{AttributeToAuthor, AttributeToMention}, Default: AttributeToAuthor,
			Desc: "Who logged time is credited to: the entry's author, or the one person its comment @mentions.",
		},
		{
			Key: "ARGUS_JIRA_SPRINT_FIELD", Section: "Jira", Kind: KindString,
			Desc: "Pin the sprint custom field id. Resolved by name when left unset.",
		},
		{
			Key: "ARGUS_JIRA_SPRINT_FIELD_NAME", Section: "Jira", Kind: KindString, Default: "Sprint",
			Desc: "The field name to resolve the sprint by.",
		},

		{
			Key: "ARGUS_SPRINT_ALLOW_WRITES", Section: "Sprint report", Kind: KindBool,
			Default: "false",
			Desc:    "Let the sprint report write: story points, assignee and worklog entries in Jira, and the report page in Confluence, each previewed and approved first. Off until you ask for it.",
		},
		{
			Key: "ARGUS_SPRINT_HOURS_PER_POINT", Section: "Sprint report", Kind: KindFloat,
			Default: "6",
			Desc:    "What one story point means in hours for this team.",
		},
		{
			Key: "ARGUS_SPRINT_HOURS_PER_DAY", Section: "Sprint report", Kind: KindFloat,
			Default: "6",
			Desc:    "A working day in hours, used to relate points to days.",
		},
		{
			Key: "ARGUS_SPRINT_ABSENCE_COST", Section: "Sprint report", Kind: KindEnum,
			Values: []string{AbsenceCostsPoint, AbsenceCostsShare}, Default: AbsenceCostsPoint,
			Desc: "What a day off costs: a whole point, or the baseline's share of one sprint day.",
		},
		{
			Key: "ARGUS_SPRINT_RECENT", Section: "Sprint report", Kind: KindInt, Default: "4",
			Desc: "How many recent sprints appear as quick buttons.",
		},
		{
			Key: "ARGUS_SPRINT_RETAIN_YEARS", Section: "Sprint report", Kind: KindInt, Default: "3",
			Desc: "How long per-person records are kept. Aggregates carry no personal data and are never pruned.",
		},

		{
			Key: "ARGUS_ATLASSIAN_ORG_ID", Section: "Atlassian team", Kind: KindString,
			Desc: "The Atlassian organisation id, for importing the roster. Optional.",
		},
		{
			Key: "ARGUS_ATLASSIAN_TEAM_ID", Section: "Atlassian team", Kind: KindString,
			Desc: "The Atlassian team id whose members the roster can be imported from. Optional.",
		},

		{
			Key: "ARGUS_CONFLUENCE_SPACE_ID", Section: "Confluence", Kind: KindString,
			Desc: "The Confluence space the sprint report pages live in. Optional; unset, publishing is not offered.",
		},
		{
			Key: "ARGUS_CONFLUENCE_PARENT_PAGE_ID", Section: "Confluence", Kind: KindString,
			Desc: "The page the sprint report pages sit under. Optional; unset, publishing is not offered.",
		},
		{
			Key: "ARGUS_CONFLUENCE_TITLE", Section: "Confluence", Kind: KindString,
			Default: "Sprint {{.Number}} Report",
			Desc:    "The page title, as a template over the sprint's Number, Name and Project.",
		},
		{
			Key: "ARGUS_CONFLUENCE_LIVE_SUFFIX", Section: "Confluence", Kind: KindString,
			Default: " (live)",
			Desc:    "Added to the title while the sprint is open, and dropped at close.",
		},
		{
			Key: "ARGUS_CONFLUENCE_SECTIONS", Section: "Confluence", Kind: KindList,
			Default: "summary,saydo,people,reasons,calibration,stories,flags,carryover,counted",
			Desc:    "The sections a published page carries, in its fixed order. The Publish panel can untick any for one publish. Publishing shares ARGUS_SPRINT_ALLOW_WRITES.",
		},

		{
			Key: "ARGUS_DATA_DIR", Section: "Where things live", Kind: KindPath,
			Default: defaultDataDir(),
			Desc:    "Where sessions and the sprint report's files go.",
		},
		{
			Key: "DATABASE_URL", Section: "Where things live", Kind: KindURL, Secret: true,
			Desc: "Use Postgres instead of files. Migrations are embedded and run on startup.",
		},
		{
			Key: "ARGUS_CONFIG", Section: "Where things live", Kind: KindPath, EnvOnly: true,
			Desc: "Read configuration from an explicit path instead of the usual search.",
		},
		{
			Key: "ARGUS_CONFIG_DIR", Section: "Where things live", Kind: KindPath, EnvOnly: true,
			Default: defaultConfigDir(),
			Desc:    "The directory holding config.env.",
		},
		{
			Key: "ARGUS_IN_CONTAINER", Section: "Where things live", Kind: KindBool,
			EnvOnly: true, Default: "false",
			Desc: "Set by the Docker image so Argus uses /data. Nothing else should set it.",
		},
	}
}

// Find looks a setting up by key, case-insensitively: someone who types
// argus_port has not misspelt anything, they have just not shouted.
func (c Catalogue) Find(key string) (Setting, bool) {
	for _, s := range c {
		if strings.EqualFold(s.Key, key) {
			return s, true
		}
	}
	return Setting{}, false
}

// Keys returns every key in the catalogue.
func (c Catalogue) Keys() []string {
	out := make([]string, 0, len(c))
	for _, s := range c {
		out = append(out, s.Key)
	}
	return out
}

// Sections returns the section names in display order, each appearing
// once, in the order they first occur.
func (c Catalogue) Sections() []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range c {
		if !seen[s.Section] {
			seen[s.Section] = true
			out = append(out, s.Section)
		}
	}
	return out
}

// Suggest returns the keys closest to what was typed, best first.
//
// A misspelt key is the whole reason this command exists: writing
// ARGUS_GITHB_ORG to the file produces a configuration that looks right,
// reads right, and does nothing at all. Naming the near miss turns that
// into a five-second fix.
func (c Catalogue) Suggest(key string) []string {
	want := strings.ToUpper(strings.TrimSpace(key))
	type scored struct {
		key  string
		dist int
	}
	var out []scored
	for _, s := range c {
		have := strings.ToUpper(s.Key)
		d := distance(want, have)
		switch {
		// A typed fragment - "port", "jira_email" - is not a misspelling
		// so much as a half-remembered name, and edit distance scores it
		// terribly. Match it on its own terms.
		case want != "" && strings.Contains(have, want):
			d = 1
		case d > 3 || d*2 > len(have):
			continue
		}
		out = append(out, scored{s.Key, d})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].dist != out[j].dist {
			return out[i].dist < out[j].dist
		}
		return out[i].key < out[j].key
	})
	keys := make([]string, 0, len(out))
	for _, s := range out {
		keys = append(keys, s.key)
		if len(keys) == 3 {
			break
		}
	}
	return keys
}

// distance is the Levenshtein edit distance. Three rows of arithmetic
// beats a dependency for a list this short.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// Origin says where a value in force came from.
type Origin string

const (
	FromDefault Origin = "default"
	FromFile    Origin = "file"
	FromEnv     Origin = "environment"
)

// Resolution is one setting's current state: what it is, where it came
// from, and what the layers underneath say.
//
// The layers matter. "I set it and nothing happened" is nearly always an
// environment variable shadowing the file, and only something that can
// see both can say so.
type Resolution struct {
	Setting Setting
	Value   string
	Origin  Origin

	// InFile and InEnv are what each layer says, whether or not it is the
	// layer in force.
	InFile string
	InEnv  string

	// Shadowed reports that the file sets this and the environment is
	// winning, which is the case worth saying out loud.
	Shadowed bool
}

// Resolve works out a setting's effective value from the environment and
// a file's contents, in the order Load applies them.
//
// It reads the environment directly, so it must be called before Load
// has copied the file into it - after that the two layers are one and
// nothing can tell them apart. `argus config` is the only caller, and it
// deliberately does not Load.
func (s Setting) Resolve(file map[string]string) Resolution {
	r := Resolution{Setting: s, InFile: strings.TrimSpace(file[s.Key])}
	if v, ok := os.LookupEnv(s.Key); ok {
		r.InEnv = strings.TrimSpace(v)
	}
	switch {
	case r.InEnv != "":
		r.Value, r.Origin = r.InEnv, FromEnv
		r.Shadowed = r.InFile != "" && r.InFile != r.InEnv
	case r.InFile != "":
		r.Value, r.Origin = r.InFile, FromFile
	default:
		r.Value, r.Origin = s.Default, FromDefault
	}
	return r
}

// Display renders a value for printing, masking it if it is a credential.
func (s Setting) Display(v string) string {
	if v == "" || !s.Secret {
		return v
	}
	if s.Kind == KindURL {
		return redactURL(v)
	}
	return mask(v)
}

// mask keeps enough of a credential to recognise which one it is, and no
// more. The length is shown because a truncated paste is a real failure
// mode and otherwise an invisible one.
func mask(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + strings.Repeat("*", 8) + fmt.Sprintf(" (%d characters)", len(v))
}

// redactURL hides the password in a connection string and leaves the rest
// legible: the host and database are what you want to check, and they are
// not the secret part.
func redactURL(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return mask(v)
	}
	if u.User == nil {
		return v
	}
	if _, set := u.User.Password(); !set {
		return v
	}
	// Swapped in the raw text rather than rebuilt through url.User, which
	// percent-encodes the mask into %2A%2A%2A... and reads as corruption
	// rather than as redaction.
	return strings.Replace(v, u.User.String(), url.User(u.User.Username()).String()+":********", 1)
}

// Validate checks a value against the setting's type.
//
// The point is to fail here, with the person present, rather than at the
// next startup with a parse error naming a line number. Anything this
// accepts, the server will accept too.
func (s Setting) Validate(v string) error {
	if strings.ContainsAny(v, "\n\r") {
		return fmt.Errorf("%s cannot span more than one line", s.Key)
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("%s needs a value. Use `argus config unset %s` to return it to its default",
			s.Key, s.Key)
	}

	switch s.Kind {
	case KindInt:
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("%s is a whole number, and %q is not one", s.Key, v)
		}
	case KindPort:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s is a port number, and %q is not one", s.Key, v)
		}
		if n < 1 || n > 65535 {
			return fmt.Errorf("%s is a port number, so it must be between 1 and 65535", s.Key)
		}
	case KindFloat:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("%s is a number, and %q is not one", s.Key, v)
		}
	case KindBool:
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on", "0", "false", "no", "off":
		default:
			return fmt.Errorf("%s is true or false, and %q is neither "+
				"(true/yes/on/1 and false/no/off/0 are all understood)", s.Key, v)
		}
	case KindEnum:
		for _, allowed := range s.Values {
			if strings.EqualFold(allowed, v) {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %s, and %q is not",
			s.Key, strings.Join(s.Values, ", "), v)
	case KindURL:
		u, err := url.Parse(v)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s is a full URL with a scheme and a host, e.g. %s",
				s.Key, urlExample(s.Key))
		}
	case KindList:
		for _, part := range strings.Split(v, ",") {
			if strings.TrimSpace(part) != "" {
				return nil
			}
		}
		return fmt.Errorf("%s is a comma-separated list, and %q has nothing in it", s.Key, v)
	case KindString:
		// The organisation goes straight into an API path, and the usual
		// mistake is pasting the address bar instead of the name. That
		// buys an empty dashboard and no explanation, so it is worth
		// catching here.
		if s.Key == "ARGUS_GITHUB_ORG" && strings.ContainsAny(v, "/. ") {
			return fmt.Errorf(
				"%s is the bare name - the part after github.com/ - not %q", s.Key, v)
		}
	}

	// Whatever is accepted here has to survive being written to the file
	// and read back. The reader strips surrounding quotes, so a value
	// that begins or ends with one cannot be represented at all, and
	// writing it anyway would store something other than what was typed.
	if _, back, ok := parseLine(s.Key + "=" + quote(v)); !ok || back != v {
		return fmt.Errorf(
			"%s cannot hold %q: the configuration file strips quotes from around a value", s.Key, v)
	}
	return nil
}

func urlExample(key string) string {
	if key == "DATABASE_URL" {
		return "postgres://user:pass@localhost:5432/argus"
	}
	return "https://example.atlassian.net"
}
