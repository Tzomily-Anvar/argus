# The sprint report

A second tool, **off by default**:

```bash
ARGUS_TOOLS=pr,sprint
```

It needs Jira credentials and somewhere to store what Jira has no source
for. Left unset it costs nothing — no database is opened, no Jira call is
made, and the tool does not appear in the rail.

What it computes, for any sprint on your board:

| Section | |
|---|---|
| **Delivered** | Points completed, excluding container types whose points are a rollup of the work beneath them |
| **Per person** | Baseline, capacity, delivered and delta — click a row for the tickets behind the number |
| **Where the sprint went** | Delivery per epic, with a Run-versus-Build style split you configure |
| **Stories concluded** | Containers that finished, reported separately because their points are not anyone's capacity |
| **Needs a look** | Finished without an estimate, finished unassigned, a container carrying its own points. These rows are what a close-out's change set is built from |
| **Carryover** | Still open at the end |

Two things have no source in Jira and are entered in the app: each
person's **baseline**, and how much of a sprint they were **away**.
Absence is split by whether it was foreseeable — planned leave should
already be in the plan, unplanned absence is what explains a shortfall.
The reason is deliberately not recorded: "unplanned" carries the whole
signal without the tool becoming a health record. A day off costs one
point by default, since a point is a day; set
`ARGUS_SPRINT_ABSENCE_COST=share` and it costs the baseline's share of one
sprint day instead, so a lead on 3 points over 10 days keeps 2.7 after a
day away rather than 2.

Points go to whoever the worklog says did the work, and to the final
assignee only when nothing was logged. On carryover the assignee is often
just whoever the ticket is parked with. Who the worklog says depends on
the team: by default the entry's **author**, which is right where people
log their own time. Set `ARGUS_JIRA_WORKLOG_ATTRIBUTION=mention` for a
team where a lead logs everyone's carryover at sprint close — Jira has no
way to log time on somebody's behalf, so the lead authors every entry and
@mentions the person who did the work, and under this mode that mention
is the credit. An entry naming several people divides between them by
the figure written beside each name — `@Person A 3h @Person B 1h`, in
hours, days or points — and equally where no figure is written, which the
report flags as a guess to be corrected. Figures that do not add up to the
time logged are flagged too: the entry is still divided by them, but one of
the two numbers is wrong and Jira reports the logged one.

Your team's conventions are configuration, not code — which issue types
are containers, which are estimated only once resolved, which statuses
mean done, and what a point is worth in hours. See `.env.example`.

### Closing out a sprint

The report tells you what is wrong; closing a sprint used to mean opening
each ticket and typing the number the report already showed you. With

```bash
ARGUS_SPRINT_ALLOW_WRITES=true
```

the report can do that part itself. It is off by default, and with it
off the report only ever reads Jira, exactly as the pull request tool
only ever reads GitHub.

**What it can write.** Five things, and nothing else:

| Operation | Where | When |
|---|---|---|
| set story points | a finished Task or Bug with none, or a Story whose linked work is all sized | the number you type, or the sum beneath a Story |
| set the assignee | a finished ticket with nobody on it | the person you pick from the roster |
| add a worklog entry | a ticket still open at close | one person, their hours, a date inside the sprint |
| correct a worklog entry | an entry on such a ticket | hours, date or the person named |
| remove a worklog entry | an entry on such a ticket | |

It does not transition status, comment, edit summaries or descriptions,
move anything between sprints, or touch GitHub. The Jira client refuses
every request outside that list before it leaves the process, whether
writes are on or off.

**Preview first, always.** Your requests are gathered into a change set
and shown as a table - ticket, field, what it holds, what it will hold -
with everything that was asked for and is *not* being changed listed
underneath with the reason. The preview lasts fifteen minutes and is
applied once; a second click finds nothing. With writes off the same
preview appears, so you can judge what the tool would do before
switching it on.

**Checked again at the moment of writing.** Each ticket is re-read
immediately before its change goes out. If somebody edited it since the
preview - the field filled in, the status moved, time logged - that one
row is skipped and says so, and the rest of the batch continues. Three
permission failures in a row stop the batch, because the token cannot
write at all and the remaining rows would fail the same way.

**Everything is logged.** Every attempt leaves a row in the same store
as the capacity figures: ticket, operation, what was there, what was
written, applied or skipped or failed, and the account it was done as.
`GET /api/sprint/writes` lists it, newest first. It is pruned on the
same three-year schedule as the per-person records.

**And it can be undone.** A change set can be reversed from its log
rows: each applied write becomes the write that puts back what was
there, previewed and approved like any other. A field somebody has since
changed by hand is left alone. A removed worklog entry comes back as a
new entry with a new id, and the preview says so.

**It is you doing it.** Edits are made with your token, so Jira shows
your name on every one. Tell the team before the first close-out.

### Publishing

The last step of a close-out is the page. The report can be published to
Confluence as one page per sprint under a parent page you name:

```bash
ARGUS_CONFLUENCE_SPACE_ID=<the space's id>
ARGUS_CONFLUENCE_PARENT_PAGE_ID=<the parent page's id>
```

Both identify your wiki, so neither has a default, and with either unset
the Publish button is not offered at all. Publishing is a write and
shares `ARGUS_SPRINT_ALLOW_WRITES`: with writes off the preview still
renders, and the button is the line naming the setting. Both ids are in
a page's URL in Confluence, or under "Page information".

**One page per sprint.** The page is titled from a template,
`ARGUS_CONFLUENCE_TITLE`, over the sprint's `Number`, `Name` and
`Project` - `Sprint {{.Number}} Report` by default. While the sprint is
open the title carries `ARGUS_CONFLUENCE_LIVE_SUFFIX` (` (live)`), and
at close the same page is renamed without it. The first publish creates
the page; every later one updates it. The page's id is remembered with
the sprint, so a page somebody renames is still found and a second one
is never created by accident; a remembered page that has been deleted is
reported rather than silently recreated.

**The markers.** Argus owns only the part of the page between two
comments:

```
<!-- ARGUS:REPORT:START -->  ...  <!-- ARGUS:REPORT:END -->
```

On an update only that block is replaced. Everything you write around it
- a scaffold above, notes and decisions below - survives every
re-publish. A page carrying the earlier tool's markers
(`SPRINT-REPORT:AUTO`) is recognised and its markers replaced with these
on the first publish, and a page with no markers has the block appended
and the preview says so. The Jira client refuses to send a page body
without exactly one pair of markers, so it can never write a page it
could not update again.

**The sections.** The page is a fixed sequence, each part of which can be
left out:

| Section | What it holds |
|---|---|
| `summary` | baseline, capacity, delivered, the say/do line |
| `saydo` | promised, injected, delivered, both ratios |
| `people` | the per-person table |
| `reasons` | the Reason column on that table, from the capacity note |
| `calibration` | estimate against actual, and the tickets that moved furthest |
| `stories` | containers concluded this sprint |
| `flags` | "Needs a look" |
| `carryover` | still open at close |
| `counted` | the "How this was counted" footer naming your conventions |

`ARGUS_CONFLUENCE_SECTIONS` is the team's standing choice, all of them by
default. The Publish panel shows the same list as switches, pre-filled
from the setting, and an untick there applies to that one publish only.
`reasons` is the one section that puts free text about a named colleague
on the page; it is what the team's pages have always carried, and it is
your choice each time.

**Preview first, always.** The preview is the rendered block beside the
block currently on the page, as a line-by-line difference, with the
action (create or update), the title and whether it changes, the version
being replaced, and the page's link. It lasts fifteen minutes and is
published once. The page is updated at the version the preview read, so
a colleague's edit in between is a refusal - from Argus first, and from
Confluence itself if that were ever missed - rather than an overwrite.
Each publish leaves one row in the same audit log as the Jira writes:
the page, the version replaced, the version written, the digest and the
title. The block's previous content is not copied there; Confluence
keeps the page's history itself.

### Storage

Files by default, in a volume, so nobody has to run a database. Set
`DATABASE_URL` and it uses Postgres instead, with migrations embedded in
the binary and applied on startup.

Per-person records are pruned after three years
(`ARGUS_SPRINT_RETAIN_YEARS`). The aggregates that trends are drawn from
carry no personal data and are kept.
