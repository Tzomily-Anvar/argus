package sprint

// The catalogue of conventions: every assumption the sprint report makes
// about how a team uses Jira, with how it is established and where it is
// explained.
//
// A convention is wrong in a way you cannot see. A wrong one produces a
// plausible number rather than an error, so the defence is that every
// one of them is stated somewhere a person can find it. This list is
// that statement in code, and docs/sprint-conventions.md is the same
// statement for a reader; a test holds the two together, and holds both
// to config.Core(), so a sprint setting added later has to be catalogued
// and documented before it builds.
//
// Nothing reads this list to decide anything. It describes behaviour, it
// does not configure it.

// How a convention is established, in the order to prefer them.
const (
	// HowDeclared means Jira's configuration states it. Read, preset,
	// and overridable for the team whose Jira says something they do not
	// mean.
	HowDeclared = "declared"

	// HowAsked means no configuration answers it: a fact about how the
	// team works rather than how their Jira is set up.
	HowAsked = "asked"

	// HowSetting means it differs between teams, changes no headline
	// figure, and has a defensible default. Documented, never asked.
	HowSetting = "setting"

	// HowFixed means it is the report's definition of what it measures,
	// not a fact about the team. Documented with its reasoning, never a
	// setting.
	HowFixed = "fixed"

	// HowApp means it is data entered in the dashboard, because it is
	// about a person and changes every sprint.
	HowApp = "app"
)

// Convention is one assumption, described.
type Convention struct {
	// Key names the convention, stable across renames of the setting.
	Key string

	// Setting is the environment key that holds or overrides it, empty
	// for one that has none.
	Setting string

	// How is one of the How constants above.
	How string

	// Source names the Jira read a declared value comes from, so "why
	// does it think this?" has an answer.
	Source string

	// Doc is the anchor of the section in docs/sprint-conventions.md.
	Doc string

	// Summary is one line: what it controls.
	Summary string
}

// Conventions is every convention the sprint report carries.
var Conventions = []Convention{
	// Identity: which Jira, as whom, and which project. Not conventions,
	// but they sit in the same section and the test insists on them.
	{Key: "jira.site", Setting: "ARGUS_JIRA_BASE_URL", How: HowSetting,
		Doc: "#which-site-and-which-project", Summary: "The Jira site the sprints live on."},
	{Key: "jira.email", Setting: "ARGUS_JIRA_EMAIL", How: HowSetting,
		Doc: "#which-site-and-which-project", Summary: "The account the report reads Jira as."},
	{Key: "jira.token", Setting: "ARGUS_JIRA_TOKEN", How: HowSetting,
		Doc: "#which-site-and-which-project", Summary: "That account's API token."},
	{Key: "jira.project", Setting: "ARGUS_JIRA_PROJECT", How: HowSetting,
		Doc: "#which-site-and-which-project", Summary: "The project whose sprints are reported."},

	// Statuses.
	{Key: "statuses.delivered", Setting: "ARGUS_JIRA_DONE_STATUSES", How: HowDeclared,
		Source: "project statuses, statusCategory.key = done",
		Doc:    "#delivered-statuses", Summary: "Which statuses count as delivered."},
	{Key: "statuses.in-flight", How: HowDeclared,
		Source: "project statuses, statusCategory.key = indeterminate",
		Doc:    "#in-flight-and-never-started-statuses", Summary: "Which statuses mean somebody is working on it."},
	{Key: "statuses.never-started", How: HowDeclared,
		Source: "project statuses, statusCategory.key = new",
		Doc:    "#in-flight-and-never-started-statuses", Summary: "Which statuses mean nobody has picked it up."},
	{Key: "statuses.board-columns", How: HowDeclared,
		Source: "board configuration, columnConfig.columns[].statuses[]",
		Doc:    "#board-columns", Summary: "Corroboration for the categories; nothing is built from it."},

	// Points.
	{Key: "points.field", Setting: "ARGUS_JIRA_POINTS_FIELD", How: HowDeclared,
		Source: "board configuration, estimation.field.fieldId",
		Doc:    "#which-field-holds-points", Summary: "The field every figure is built from."},
	{Key: "points.field-name", Setting: "ARGUS_JIRA_POINTS_FIELD_NAME", How: HowSetting,
		Doc: "#which-field-holds-points", Summary: "The display name the points field is resolved by where no board declares one."},
	{Key: "points.board-estimates", How: HowDeclared,
		Source: "board configuration, estimation.type",
		Doc:    "#whether-the-board-uses-points", Summary: "Whether the board estimates in points at all."},
	{Key: "points.estimate-field", Setting: "ARGUS_JIRA_ESTIMATE_FIELD_NAME", How: HowSetting,
		Doc: "#the-fallback-estimate-field", Summary: "The field read when finished work has no points; flagged when used."},
	{Key: "sprint.field", Setting: "ARGUS_JIRA_SPRINT_FIELD", How: HowSetting,
		Doc: "#which-field-holds-the-sprint", Summary: "The field saying which sprints an issue has been on."},
	{Key: "sprint.field-name", Setting: "ARGUS_JIRA_SPRINT_FIELD_NAME", How: HowSetting,
		Doc: "#which-field-holds-the-sprint", Summary: "The display name that field is resolved by."},

	// Structure.
	{Key: "types.epic-level", How: HowFixed, Source: "the issue's parent field",
		Doc: "#epic-level-containers-and-subtasks", Summary: "Epics contain work by construction."},
	{Key: "types.subtasks", How: HowFixed, Source: "the issue's parent field",
		Doc: "#epic-level-containers-and-subtasks", Summary: "Subtasks are children by construction."},
	{Key: "types.container", Setting: "ARGUS_JIRA_CONTAINER_TYPES", How: HowAsked,
		Doc: "#a-standard-level-container", Summary: "Which standard-level types roll up work beneath them."},
	{Key: "types.child-links", Setting: "ARGUS_JIRA_STORY_LINK_TYPES", How: HowAsked,
		Doc: "#which-links-mean-is-part-of", Summary: "Which link types tie a container to its work."},
	{Key: "types.estimated-on-resolve", Setting: "ARGUS_JIRA_ESTIMATED_ON_RESOLVE", How: HowSetting,
		Doc: "#estimated-on-resolve", Summary: "Types sized when the work finishes; changes flags, never totals."},
	{Key: "types.excluded-from-say-do", Setting: "ARGUS_JIRA_EXCLUDED_TYPES", How: HowSetting,
		Doc: "#excluded-from-saydo", Summary: "Types carrying no commitment of their own."},
	{Key: "epics.classes", Setting: "ARGUS_JIRA_EPIC_CLASSES", How: HowSetting,
		Doc: "#how-epics-are-grouped", Summary: "How epics are labelled for the delivery split."},

	// Time and capacity.
	{Key: "sprint.length", Setting: "ARGUS_JIRA_SPRINT_LENGTH_DAYS", How: HowSetting,
		Doc: "#sprint-length-in-working-days", Summary: "Working days in a sprint, for reading a baseline against."},
	{Key: "points.hours-per-point", Setting: "ARGUS_SPRINT_HOURS_PER_POINT", How: HowAsked,
		Doc: "#what-a-point-is-worth", Summary: "What one point is in hours, for turning logged time into points."},
	{Key: "points.hours-per-day", Setting: "ARGUS_SPRINT_HOURS_PER_DAY", How: HowAsked,
		Doc: "#what-a-point-is-worth", Summary: "A working day in hours, for a figure written in days."},
	{Key: "capacity.absence-cost", Setting: "ARGUS_SPRINT_ABSENCE_COST", How: HowSetting,
		Doc: "#what-a-day-off-costs", Summary: "A day off costs a point, or the baseline's share of a sprint day."},
	{Key: "worklog.attribution", Setting: "ARGUS_JIRA_WORKLOG_ATTRIBUTION", How: HowSetting,
		Doc: "#who-logged-time-is-credited-to", Summary: "Logged time goes to the entry's author or to the person it @mentions."},
	{Key: "roster", How: HowApp,
		Doc: "#the-roster-baselines-and-availability", Summary: "Who is measured, opted in or out, and each baseline."},
	{Key: "capacity.availability", How: HowApp,
		Doc: "#the-roster-baselines-and-availability", Summary: "Days off, the reviewed marker and the note, per person per sprint."},
	{Key: "roster.import-org", Setting: "ARGUS_ATLASSIAN_ORG_ID", How: HowSetting,
		Doc: "#roster-import-identifiers", Summary: "The Atlassian organisation the roster may be proposed from."},
	{Key: "roster.import-team", Setting: "ARGUS_ATLASSIAN_TEAM_ID", How: HowSetting,
		Doc: "#roster-import-identifiers", Summary: "The Atlassian team the roster may be proposed from."},

	// Fixed: what the report measures.
	{Key: "points.fallback-order", How: HowFixed,
		Doc: "#points-fallback-order", Summary: "Board's field, then the fallback, then zero, each fallback flagged."},
	{Key: "points.unestimated-is-zero", How: HowFixed,
		Doc: "#unestimated-counts-as-zero", Summary: "Finished work with no points contributes nothing and is flagged."},
	{Key: "carryover.split-by-worklog", How: HowFixed,
		Doc: "#how-a-carried-ticket-is-split", Summary: "A ticket spanning sprints divides its points by time logged in each."},
	{Key: "carryover.equal-split", How: HowFixed,
		Doc: "#how-a-carried-ticket-is-split", Summary: "With no worklog, the sprints it was active in share it equally."},
	{Key: "attribution", How: HowFixed,
		Doc: "#attribution", Summary: "Points go to whoever logged the work, else the final assignee."},
	{Key: "dating.completion", How: HowFixed, Source: "the sprint's completeDate",
		Doc: "#when-a-ticket-concluded", Summary: "A ticket counts where it concluded, by the sprint's real close."},
	{Key: "dating.final-run", How: HowFixed,
		Doc: "#when-a-ticket-concluded", Summary: "A ticket concluded when its last unbroken delivered run began."},
	{Key: "container.concludes-with-children", How: HowFixed,
		Doc: "#a-container-concludes-with-its-children-and-is-rolled-up-at-close", Summary: "A container is finished only when everything beneath it is."},
	{Key: "container.rollup-at-close", How: HowFixed,
		Doc: "#a-container-concludes-with-its-children-and-is-rolled-up-at-close", Summary: "A container's points become the sum beneath it at close-out, when all sized."},
	{Key: "say-do.by-count", How: HowFixed,
		Doc: "#saydo-by-ticket-count", Summary: "Promised versus delivered in tickets, not points."},
	{Key: "capacity.planned-only", How: HowFixed,
		Doc: "#capacity-deducts-planned-leave-only", Summary: "Capacity deducts planned leave; unplanned absence explains a shortfall."},

	// Not conventions, catalogued so the section is complete.
	{Key: "writes", Setting: "ARGUS_SPRINT_ALLOW_WRITES", How: HowSetting,
		Doc: "#settings-that-are-not-conventions", Summary: "Whether the close-out may write to Jira and Confluence."},
	{Key: "recent", Setting: "ARGUS_SPRINT_RECENT", How: HowSetting,
		Doc: "#settings-that-are-not-conventions", Summary: "How many sprints get a quick button."},
	{Key: "retain", Setting: "ARGUS_SPRINT_RETAIN_YEARS", How: HowSetting,
		Doc: "#settings-that-are-not-conventions", Summary: "How long per-person records are kept."},
}
