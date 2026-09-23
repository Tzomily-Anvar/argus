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
| **Needs a look** | Finished without an estimate, finished unassigned, a container carrying its own points |
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
report flags as a guess to be corrected.

Your team's conventions are configuration, not code — which issue types
are containers, which are estimated only once resolved, which statuses
mean done, and what a point is worth in hours. See `.env.example`.

### Storage

Files by default, in a volume, so nobody has to run a database. Set
`DATABASE_URL` and it uses Postgres instead, with migrations embedded in
the binary and applied on startup.

Per-person records are pruned after three years
(`ARGUS_SPRINT_RETAIN_YEARS`). The aggregates that trends are drawn from
carry no personal data and are kept.
