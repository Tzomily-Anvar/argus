# The sprint report's conventions

Argus reads your Jira's configuration — the categories your statuses
declare, the field your board estimates in, the levels your issue types
sit at. It does not learn anything from your tickets. So a value it found
is as correct as your Jira setup is, and where your setup does not mean
what it says, the override is there.

Conventions live in the configuration file; the roster, baselines and
each sprint's availability live in the app. A newer Argus reads older
data; an older Argus does not read newer data.

Each section says how its convention is established:

| | |
|---|---|
| **read from Jira** | declared by your Jira's configuration and preset from it; the setting beside it is an override, normally unset |
| **asked** | a fact about how the team works that no configuration declares |
| **a setting** | differs between teams, changes no headline figure, has a defensible default; in `.env.example`, never asked |
| **fixed** | the report's definition of what it measures, not a fact about the team; documented with its reasoning, never a setting |
| **in the app** | entered through the dashboard, because it is about a person and changes every sprint |

Today, read from Jira: the three status groups (delivered, in flight,
never started), the field the board estimates in, and whether it
estimates in points at all. Everything else with a key is still a
setting. `internal/sprint/conventions.go` says the same in code, and a
test holds it to this page.

A wrong convention does not produce an error; it produces a plausible
number. So each entry ends with what that number looks like.

## Which site and which project

`ARGUS_JIRA_BASE_URL`, `ARGUS_JIRA_EMAIL`, `ARGUS_JIRA_TOKEN` and
`ARGUS_JIRA_PROJECT`: which Jira, as whom, which project. Settings with
no default, because a default would name somebody's company. Unset, the
sprint tool stays hidden rather than failing.

**Wrong, it looks like:** no report, or a report on the wrong project.
Visible, which is why these are not conventions.

## Delivered statuses

Which statuses mean the work is finished; every dating and crediting
decision downstream depends on it.

**Read from Jira**, from `statusCategory` on the project's statuses:
every status declares `new`, `indeterminate` or `done`, and the `done`
ones are delivered. A team may disagree with its own category — a status
meaning cancelled or rejected sits in `done` on some sites — and for that
team `ARGUS_JIRA_DONE_STATUSES` names the delivered statuses outright,
replacing the declared list rather than adding to it. A status meaning
superseded is deliberately not the case for it: work was done to reach
that conclusion, and excluding it makes the effort vanish.

**Wrong, it looks like:** delivery reads low, and work in your real
terminal status is carryover for ever. The other way: cancelled tickets
appear as delivered points.

## In-flight and never-started statuses

Telling work somebody picked up and could not finish from work nobody
touched. Both are open at the end; only the first is work that happened,
and only the first earns a share of a carried ticket.

**Read from Jira**, from the same `statusCategory`: `indeterminate` is
in flight, `new` is never started. No override. A status id nobody
recognises reads as in flight on purpose: the two states that can be
positively identified are "not picked up" and "finished", and guessing
the other way would report a sprint in which nothing happened.

**Wrong, it looks like:** a status you treat as active whose category is
`new` reads as never started, so "never started" is high and carried
tickets lose their share.

## Board columns

Nothing is built from them. The team's arrangement of statuses into
columns is corroboration: on a tidy board every column's statuses share
one category. The tempting rule — the last column is delivery — is wrong,
because a team whose delivered statuses span the last two columns would
silently lose one. A column whose statuses cross categories is two
declared sources disagreeing, which is for a person to see and not for a
tool to resolve.

**Wrong, it looks like:** it cannot make a number wrong.

## Which field holds points

The custom field every figure is built from.

**Read from Jira**, from the board's configuration: `estimation.field`
names the field the team chose to estimate in. That replaces resolving it
by display name, which breaks on a site that renamed the field and is
ambiguous on one carrying both `Story Points` and `Story point estimate`.
`ARGUS_JIRA_POINTS_FIELD` pins a field id (`customfield_10000`) where the
board's choice is not the one you mean; `ARGUS_JIRA_POINTS_FIELD_NAME` is
the display name used where no board declares one.

**Wrong, it looks like:** every total is off by whatever the other field
says, in every sprint. Half the team's work unsized is the same fault
from the other side: the board estimates in a field nobody fills in.

## Whether the board uses points

**Read from Jira**, from `estimation.type` on the board: `field` means
points, `issueCount` means the team does not estimate in points. No
override: a board without points has none, and the report says so at the
top, keeps delivery, say/do and carryover by ticket count, and hides the
capacity figures.

**Wrong, it looks like:** zero-point rows everywhere and a capacity
table nobody can read.

## The fallback estimate field

What is read when a finished ticket has nothing in the board's field.
**A setting**, `ARGUS_JIRA_ESTIMATE_FIELD_NAME`, default `Story point
estimate`, and deliberately not detected: a second populated field is
usage, not configuration. A fallback, not a second opinion, and every
ticket it is used on is flagged (`estimate_used`).

**Wrong, it looks like:** finished work with no estimate flagged every
sprint on a team that sizes somewhere else.

## Which field holds the sprint

The field saying which sprints an issue has been on the board of, which
a carried ticket's share is divided across. **A setting**: resolved by
`ARGUS_JIRA_SPRINT_FIELD_NAME` (default `Sprint`), pinned with
`ARGUS_JIRA_SPRINT_FIELD`. One site in a thousand renames it.

**Wrong, it looks like:** nothing builds; the report refuses rather than
guessing where an issue has been.

## Epic-level containers and subtasks

Types that contain work by construction. An Epic reaches its work through
the `parent` field and a subtask its parent the same way. **Fixed**: Jira
declares these and nothing about them differs between teams. Reading
`hierarchyLevel` from the type catalogue is where this is headed.

**Wrong, it looks like:** it goes wrong only through the next entry.

## A standard-level container

Whether one standard-level type acts as a container over another — a
Story holding work done as Tasks — and which. The deepest assumption in
the model. **Asked**, through `ARGUS_JIRA_CONTAINER_TYPES` (default
`Story`), because peers at the same level are identical to the type
scheme and nothing declares the relationship. A container's points are a
rollup of the work beneath it, reported as work concluded and never as
delivery. The default is a convention, not a fact: on a board where
Stories are the unit of delivery it should be empty.

**Wrong, it looks like:** delivery roughly double what the team believes
(a rollup type not named, so Story and Tasks both count), or roughly half
with "Stories concluded" full (a delivery type named as a container).

## Which links mean "is part of"

How a standard-level container reaches its children. Jira's `parent` is
taken by the Epic, so the Story-to-Task association lives in issue links,
and which types carry it is a team's habit. **Asked**, through
`ARGUS_JIRA_STORY_LINK_TYPES`, from the link types Jira declares. Jira
declares the names, never the meaning, so Argus never pre-selects the
most used one: a type left behind by a migration looks exactly like a
deliberate one by frequency.

**Wrong, it looks like:** every container reports no defined work
(`story_no_work` on all of them) when a type is missing; unrelated
tickets beneath Stories when a generic type such as `Relates` is in. The
second is a permanent trade, and the flag keeps it visible.

## Estimated on resolve

Types whose points are recorded when the work finishes rather than when
it is planned. **A setting**, `ARGUS_JIRA_ESTIMATED_ON_RESOLVE`, default
`Bug`, never asked: it changes which tickets are flagged, never a total.
Not inferred from changelogs, because that would be reading usage.

**Wrong, it looks like:** open Bugs without points flagged every sprint,
or a type that should be sized up front never flagged while open.

## Excluded from say/do

Types carrying no commitment of their own. **A setting**,
`ARGUS_JIRA_EXCLUDED_TYPES`, default `Epic`: a container is excluded for
being a container and nothing else.

**Wrong, it looks like:** say/do reads low by about the same amount every
sprint, carrying Epics as promises nobody made.

## How epics are grouped

The Run-versus-Build split in "Where the sprint went". **A setting**,
`ARGUS_JIRA_EPIC_CLASSES`, `name:pattern` pairs matched against the
epic's summary. It labels groups and totals nothing.

**Wrong, it looks like:** every epic in no class, or in one.

## Sprint length in working days

Context for reading a baseline, and the divisor when a day off is priced
as a share. **A setting** today, `ARGUS_JIRA_SPRINT_LENGTH_DAYS`, default
`10`; the sprint's own dates are what should settle it, and will.

**Wrong, it looks like:** under the share rule every day off costs the
wrong fraction; under the point rule only the stated length is wrong.

## What a point is worth

`ARGUS_SPRINT_HOURS_PER_POINT` and `ARGUS_SPRINT_HOURS_PER_DAY`, both
default `6`. **Asked**, because it is an agreement, not anything Jira
knows. They turn logged time into points — how a sprint is credited for
effort on a ticket that finished elsewhere — and read a figure written in
days beside a name in a worklog comment.

**Wrong, it looks like:** carried tickets earn too much or too little
for the hours logged, and finished ones hand the wrong amount back to
earlier sprints, by one constant factor.

## What a day off costs

**A setting**, `ARGUS_SPRINT_ABSENCE_COST`: `point` takes a whole point
per day, because a point is a day; `share` takes the baseline's share of
one sprint day, for somebody whose three points are spread over ten days.
Never asked, and the report states which mode produced it.

**Wrong, it looks like:** a part-timer's capacity is nearly all absence
under `point`; a full-timer's absence is cheap under `share`.

## Who logged time is credited to

**A setting**, `ARGUS_JIRA_WORKLOG_ATTRIBUTION`: `author`, the account
that logged the entry, or `mention`, the person the comment @mentions,
for a team whose lead logs everyone's time at close — Jira cannot log
time on somebody's behalf, so the mention is where the real person is
named. Several names divide by the figure beside each, and equally where
there is none, which is flagged.

**Wrong, it looks like:** the lead delivered everything that was
carried, and everybody else's carried work is zero.

## The roster, baselines and availability

Who is measured and opted in, each baseline, and per sprint each
person's planned and unplanned days off, the reviewed marker and the
note. **In the app**: a baseline is a judgement about a person that
changes when somebody goes part-time or is lent out, and availability is
one row per person per sprint. Delivery is not gated on any of it —
points delivered by anybody count towards the sprint; being measured
against a baseline is a separate question.

**Wrong, it looks like:** `no_baseline` or `delivered_off_roster` on a
person, per-person figures not summing to the total, and
`capacity_unreviewed` with every capacity equal to its baseline.

## Roster import identifiers

`ARGUS_ATLASSIAN_ORG_ID` and `ARGUS_ATLASSIAN_TEAM_ID`, which Atlassian
team the roster may be proposed from. **Settings** with no default that
must never acquire one: they identify an organisation. Unset, the import
is not offered rather than offered and broken.

**Wrong, it looks like:** the wrong people proposed, in the panel,
before anything is accepted.

## Points fallback order

**Fixed.** The board's field, then the configured fallback, then zero,
each fallback flagged. A silent fallback is how a total becomes
unexplainable; the flag is the feature. Where the report asks what
somebody actually recorded — a container against the work beneath it —
the board's field alone answers, with no fallback.

**Wrong, it looks like:** it cannot be, because every step is visible.

## Unestimated counts as zero

**Fixed.** A finished ticket with no points contributes nothing and is
flagged (`done_no_estimate`). Guessing a median would invent delivery;
zero plus a flag is honest, and the close-out turns the flag into the
write that fixes it.

**Wrong, it looks like:** delivery low with a long flag list. The fix is
in Jira, not in a setting.

## How a carried ticket is split

**Fixed.** A ticket that spans sprints divides its points by the time
logged in each: one finishing here hands back what was logged before it
opened, one still open earns what was logged here, and the shares sum to
its points and never more. With no time logged anywhere, the sprints it
was *active* in share it equally — active, not merely on the board of,
because Jira's sprint field never forgets. A sprint that never picked it
up gets nothing; the one that finished it always takes a share; where the
count is unknown an open ticket earns nothing rather than the whole.
Worklog is the only evidence of where effort went, and a setting here
would change what "delivered" means and break the trend series.

**Wrong, it looks like:** `carried_no_worklog` on most of the carryover,
which is the common path rather than an error and is flagged for that
reason; and "prior points deducted" beside a total, so one that looks low
can be seen to be low for a reason.

## Attribution

**Fixed.** Points go to whoever the worklog says did the work, and to the
final assignee only when nothing was logged, because on carryover the
assignee is often whoever the ticket was parked with. A per-person number
a setting could reassign is not a measurement. Points belonging to nobody
are shown as unattributed rather than going missing.

**Wrong, it looks like:** per-person rows not summing to the total, with
the difference on the unattributed line.

## When a ticket concluded

**Fixed.** A ticket counts in the sprint whose window it reached a
delivered status in. The window shuts at the sprint's real close — the
`completeDate` of the Complete Sprint click, not the nominal end date,
which teams overrun by days — and opens at the previous sprint's close,
so the minutes between are not a hole work falls into. Within that, the
ticket concluded when its final unbroken run of delivered statuses began:
a team with several delivered statuses moves a ticket between them days
later, and a ticket reopened and finished again concluded the second
time. This is what stops the same work counting in four sprints running.

**Wrong, it looks like:** work tidied after the end date landing in
whichever sprint tidied the board; the same ticket delivered twice.

## A container concludes with its children, and is rolled up at close

**Fixed.** A Story is finished when it is done *and* everything beneath
it is, and the sprint closing the last of that work claims it; one marked
done over work in flight is flagged (`story_work_done_not_closed`). At
close-out its own points become the sum beneath it, and only when every
item is sized — otherwise no suggestion and no write, because a rollup
over unsized work would be a guess written into the velocity history. A
Story that concluded earlier reappears only while its points disagree
with the sum. All of this depends on the container and link-type answers
above, which is why those are asked.

**Wrong, it looks like:** Stories concluded in a sprint their Tasks were
not finished in; `story_points_mismatch` until the next close-out.

## Say/do by ticket count

**Fixed.** Promised versus delivered is counted in tickets, not points.
Counting points would make the ratio depend on estimates being right,
which is the thing it exists to check.

**Wrong, it looks like:** it cannot be wrong for a team, only a ratio
they wanted in another unit.

## Capacity deducts planned leave only

**Fixed.** Capacity is the baseline less what planned leave costs.
Unplanned absence is not subtracted: capacity is what the team committed
to knowing what it knew, and taking illness off it would erase the miss
it caused. It is reported instead as the part of a shortfall absence
accounts for, never more than the shortfall itself.

**Wrong, it looks like:** a sprint with illness under-delivered against
its capacity, with the "from absence" figure beside it. That is the
intended reading.

## Settings that are not conventions

`ARGUS_SPRINT_ALLOW_WRITES`, `ARGUS_SPRINT_RECENT` and
`ARGUS_SPRINT_RETAIN_YEARS` sit in the same section and change nothing
about how a number is counted: whether the close-out may write, how many
sprints get a quick button, how long per-person records are kept. Listed
so the catalogue is complete and its test can insist on every sprint
setting being named.

**Wrong, it looks like:** a button that is not there, or a record
pruned. Visible, and not numeric.
