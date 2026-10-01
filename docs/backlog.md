# The backlog

A third tool, **off by default**:

```bash
ARGUS_TOOLS=pr,sprint,backlog
```

It reads the same Jira project as the sprint report, with the same
credentials, and keeps what it remembers in the same storage. Left
unset it costs nothing and does not appear in the rail.

It answers one question: what in the open backlog needs a hand, and
what has happened to me lately. Everything it shows comes from one
sweep of the project's open tickets — `statusCategory != Done`, newest
first — refreshed on the same interval as the pull request dashboard
and slowed the same way when nobody has looked for a while. Opening the
page reads what is already in memory.

## What it shows

**New.** Tickets created in the last `ARGUS_BACKLOG_NEW_DAYS` days that
you have not acknowledged. Acknowledging one hides it until it changes
again — see below.

**Groups.** Each is a punch list with one sentence saying why it
matters, the tickets in it, and the exact JQL that opens the same set
in Jira's issue navigator.

| Group | What is in it |
|---|---|
| `stale` | No update in `ARGUS_BACKLOG_STALE_DAYS` days or more |
| `no_epic` | Work with no epic above it, so it never shows in the delivery split |
| `no_story` | A task or bug linked to no story, so it will never roll up as delivered |
| `no_labels` | Nothing says what kind of work it is |
| `unsized` | A task or bug with neither points nor an estimate |
| `no_sprint` | In no active or future sprint — the backlog proper |
| `build_epic_no_story` | Under a Build epic but linked to no story |
| `request_missing_label` | A request from outside the team without the label |
| `legacy_label` | Carrying an older spelling of the request label |
| `request_work_label` | A requester's ticket labelled as a kind of engineering work |

"A task or bug" means any work type: everything that is not a
container, an epic or a sub-task, which on a standard project is Task
and Bug. Which types are containers, which link types tie a task to its
story, and which epics are Build all come from the sprint report's
settings — `ARGUS_JIRA_CONTAINER_TYPES`, `ARGUS_JIRA_STORY_LINK_TYPES`
and `ARGUS_JIRA_EPIC_CLASSES`. A project whose hierarchy has no
container type at the standard level gets no story groups at all,
because they would list every task.

Where a group's set is expressible in JQL, the query beside it says
what the group says, so the two can be checked against each other.
Where it is not — acknowledgement is local, and a link to a story is not
a JQL clause — the query names the keys, which opens exactly the set
shown.

**Epics.** Every open epic with the open work beneath it and how much
of that is still unrefined: in a status named *To Refine*, or in Jira's
*new* category. A closed epic that still has open work under it is
listed too, because that is worth seeing rather than hiding.

**Requests.** Every request from outside the team, and above them the
ones missing the label. A typical use is an operations or support team
whose tickets engineering triages, but the tool does not assume that:
a ticket is a request by any of three signals. It carries
`ARGUS_BACKLOG_REQUEST_LABEL`; it carries one of
`ARGUS_BACKLOG_LEGACY_LABELS`; or its reporter is one of the
*requesters*, a list of account ids kept in the app. The list exists
because the label is applied by hand and forgotten, and the reporter is
the one signal that is never forgotten. It lives behind the gear, like
the sprint report's team. Candidates for it are the reporters seen in
the backlog, most frequent first, so a person is ticked rather than an
account id copied about; with `ARGUS_BACKLOG_REQUEST_TEAM_ID` set, the
Atlassian team's members are proposed too.

The legacy labels, if a team names any, are read as meaning the same
thing and are never added; the tool offers to replace them so they die
out. Work labels are the opposite kind: they mark engineering work -
`DevOps`, say, or a repository's name - and a requester's ticket
carrying one is flagged, because the person asking has named the fix
rather than the need. The same label on an engineer's own ticket is
nobody's mistake and is left alone. Only the request label is ever
written by this view, and only by the other side of the tool, behind
`ARGUS_SPRINT_ALLOW_WRITES`.

**Labels.** The open backlog one label at a time. Pick any label the
open tickets carry - the picker lists them with their counts, most used
first - and the view shows every ticket under it, with two buttons:
*Migrate to…*, which moves them all to another label in one edit each,
and *Remove*, which takes the label off and leaves the rest. Either acts
on the rows you have ticked in that list, or on all of them when none
are, and says which. This is how a spelling that drifted, or two labels
that came to mean the same thing, are folded back into one; the
Requests view's "replace the legacy label" is the same migration with
the labels filled in.

**Inbox.** A personal feed, across every project and space: issues
that mention you, are assigned to you, or that you watch, changed in
the last `ARGUS_BACKLOG_INBOX_DAYS` days; and Confluence pages that
mention you or that you watch, modified in the same window. Neither
product offers a notifications feed to read, so this is assembled from
what can be queried. Assignment and watching are exact. A mention is
exact in Confluence, whose query language has a field for it, and a
best-effort proxy in Jira — `text ~ currentUser()` — where a query that
fails becomes a warning on the page rather than an empty inbox.

## Acknowledge and dismiss

Neither writes anything to Jira. An acknowledgement is a watermark kept
locally: the ticket's `updated` timestamp exactly as Jira gave it. While
the ticket's timestamp still matches, it stays out of the New list;
the moment anybody changes the ticket its timestamp moves, the
watermark no longer matches, and it is back. That is the whole
mechanism, and it is why an acknowledgement is never "forever": it means
*I have seen this version*.

Acknowledge one ticket or a whole group; withdraw it the same way.
Dismissing an inbox item is the same mechanism under another name,
keyed by the item — the same issue mentioned and assigned is two items,
each dismissed on its own.

Acknowledgements are a record of what you looked at, and the keys they
hold are other people's tickets and pages, so retention drops any older
than ninety days. By then the item has either changed, and the row is
dead, or it has been still for three months and a fresh look at it is
no bad thing.

## Settings

| Setting | Default | |
|---|---|---|
| `ARGUS_BACKLOG_STALE_DAYS` | 14 | No update in this many days is stale |
| `ARGUS_BACKLOG_NEW_DAYS` | 3 | Created within this many days is new |
| `ARGUS_BACKLOG_REQUEST_LABEL` | `Request` | The label that marks a request from outside the team; the one label the Requests view ever adds |
| `ARGUS_BACKLOG_LEGACY_LABELS` | none | Older spellings of it, read as the same thing and never added |
| `ARGUS_BACKLOG_WORK_LABELS` | none | Labels that mark engineering work; a requester's ticket carrying one is flagged |
| `ARGUS_BACKLOG_REQUEST_TEAM_ID` | none | An Atlassian team whose members are proposed as requesters; optional |
| `ARGUS_BACKLOG_INBOX_DAYS` | 3 | How far back the inbox looks |
| `ARGUS_BACKLOG_ALLOW_DELETE` | `false` | The one irreversible write, on top of `ARGUS_SPRINT_ALLOW_WRITES` |

Deleting a ticket is the one thing the tool can do that cannot be
undone, so it has a gate of its own: both settings must be on, the
confirmation types the count, and every deleted ticket leaves an audit
row. Nothing on the read side described here is affected by either
setting; with both off the tool reads Jira and writes only its own
acknowledgements and requesters.

Which field holds points, which holds the estimate and which the sprint
are resolved the way the sprint report resolves them: the board's
estimation field first, then the field names, with `ARGUS_JIRA_POINTS_FIELD`
and `ARGUS_JIRA_SPRINT_FIELD` to pin an id. The exact spellings of
everything are in [`.env.example`](../.env.example).

Select tickets in any view of the Backlog tool and the bulk bar offers
what can be done to all of them at once. Every action is previewed, one
row per ticket, and applied through the same gate as the sprint
report's close-out. With

```bash
ARGUS_SPRINT_ALLOW_WRITES=true
```

the preview can be applied. Off - the default - the preview still
renders, so you can see what the tool would do, and the button is the
line naming the setting.

**What it can do.** Eight things, and nothing else:

| Action | What is written | Skipped when |
|---|---|---|
| assign to a sprint | the tickets moved onto the sprint you pick | already in that sprint |
| set the Epic | the parent field | already under that Epic; the ticket is an Epic |
| link to a Story | one link, of the first type in `ARGUS_JIRA_STORY_LINK_TYPES` | already linked to it; the ticket is a Story or an Epic |
| add labels | the labels you pick, added to whatever is there | already carries them all |
| remove labels | the labels you pick, taken off where present | carries none of them |
| migrate labels | the labels you name removed, the one you name added | carries none of the labels being replaced |
| add the request label | `ARGUS_BACKLOG_REQUEST_LABEL` | already carries it |
| delete | the ticket, subtasks included | the ticket is an Epic |

Labels are added and removed one at a time, never set as a whole list,
so a label somebody adds between the preview and the write survives.
A migration is one edit per ticket - the old labels off, the new one on
- and a ticket carrying none of the old ones is left alone even if it
already carries the new one, because it was never on the old. The
labels the bulk bar offers are chosen behind the gear, picked from what
the open backlog carries (most used first) or typed for one not in use
yet; with none chosen the bar asks for a spelling each time. The bar's
*Migrate label…* picks from the chosen labels and every label in use,
then where to move them. The list is the team's own and creates or
deletes nothing in Jira.
A story link is made the way the team makes them: the child at the end
`ARGUS_BACKLOG_STORY_LINK_CHILD` names (`inward` by default), the Story
at the other. It does not transition status, comment, edit summaries,
or touch GitHub.

**Preview first, always.** The preview reads every selected ticket and
shows what it holds and what it will hold, with every ticket that is
being left alone listed with its reason. It lasts fifteen minutes and is
applied once; a second click finds nothing.

**Checked again at the moment of writing.** Each ticket is re-read
immediately before its write goes out, and one that has been edited
since the preview is skipped and says so; the rest of the batch
continues. A sprint move sends up to fifty tickets in one request, each
still checked on its own first. Three permission failures in a row stop
the batch, as does a second rate limit from Jira.

**Everything is logged.** Every attempt leaves a row in the same audit
log as the sprint report's writes: ticket, action, what was there, what
was written, the outcome, and the account it was done as.

**And it can be undone**, except for delete. Reverse builds a new
preview from the batch's log rows - the label removed again or put back,
the previous Epic restored or the parent cleared, the link removed, the
ticket moved back to the sprint it came from or to the backlog - checked
against the tickets as they are now, and applied like any other. A
ticket somebody has since changed by hand is left alone.

## Deleting

Delete is the one action nothing can reverse: a deleted ticket is gone
from Jira, history and all. So it is off even with writes on, and needs

```bash
ARGUS_BACKLOG_ALLOW_DELETE=true
```

on top. With it off the preview still shows what would go and the button
names this setting. With it on, applying asks you to type the number of
tickets the preview says will be deleted - not a click, a number - and
refuses anything else. An Epic is never deleted in bulk, because the
work beneath it would be orphaned. One audit row is written per ticket,
with its summary, since that is the only place the ticket survives. There
is no reverse; the button is not offered.

**It is you doing it.** Every write is made with your token, so Jira
shows your name on every one. Tell the team before the first bulk edit.
