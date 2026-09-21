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
signal without the tool becoming a health record.

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
