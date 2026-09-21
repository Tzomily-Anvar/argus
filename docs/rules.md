# The rules

Each check is a **rule**: independent, individually switchable, and
tunable without touching code. The **Rules** button, beside Refresh,
lists every one of them live with the exact environment variable for each
knob. It reports configuration; it does not change it.

| Rule | What it surfaces |
|---|---|
| `review_requested` | Open PRs where a review is requested from you or your teams. |
| `unreviewed` | Review-ready PRs nobody has been assigned to. |
| `merge_readiness` | Every open PR with its review decision and check status. |
| `my_prs` | Your own open PRs, drafts included. |
| `stale_prs` | PRs older than a threshold, with bot PRs counted separately. |
| `stale_branches` | Branches with no commit in a long time. |
| `security` | Dependabot and code-scanning alerts, plus open Dependabot PRs. |

The dashboard's sections are not one-to-one with these rules. Some are a
filtered view of one — **Ready to merge** and **Ready to QA** split the
approved-and-green pile by whether the QA label is present, and **Stale**
shows the stale pull requests and stale branches rules together, because
they answer one question. The **Overview** stacks everything, ordered by
how much it is your move.

Turn a rule off, or retune it, in `.env`:

```bash
ARGUS_RULE_STALE_PRS_ENABLED=false      # opt out entirely
ARGUS_RULE_STALE_PRS_DAYS=21            # or just retune it
```

Then `./run.sh restart`. Every knob and its default is documented in
[`.env.example`](../.env.example), which is generated from the rules
themselves and so cannot drift from them.

### Which repositories

By default Argus sweeps the whole organisation, minus archived
repositories. Three settings narrow that, and they apply everywhere —
pull requests, branches and security alerts alike:

```bash
ARGUS_REPOS=api,web,infra            # only these; unset means all
ARGUS_EXCLUDE_REPOS=sandbox,archive  # or subtract a few
ARGUS_INCLUDE_ARCHIVED=true          # archived are skipped by default
```

The security rule takes its own list on top, for repositories whose
alerts you will never act on — template repositories, sandboxes,
interview exercises — without hiding them from the pull request views:

```bash
ARGUS_RULE_SECURITY_EXCLUDE_REPOS=interview-exercise,old-prototype
```

This matters more than it sounds. A handful of neglected repositories
can carry most of an organisation's open alerts and push the ones you
would actually fix off the bottom of the list.

### The QA stage

If your workflow has a label that gates merge, name it and Argus splits
the approved work in two: **Ready to merge** (the label is present,
nothing left to do) and **Ready to QA** (reviewed, approved and green,
with only the label outstanding — a handover, not something to chase the
author about).

```bash
ARGUS_RULE_MERGE_READINESS_QA_LABEL=qa-signoff
```

With no label configured there is no QA stage, the section does not
appear, and **Ready to merge** simply means approved and green.

### Real failures versus policy gates

Many organisations have a required check that is a *policy* rather than a
test — "this PR is missing a required label", say. It fails red exactly
like a broken build, which makes approved, working PRs look broken.

Tell Argus which checks are policy gates and it reports them separately,
so `real_failures` means something is actually wrong:

```bash
ARGUS_RULE_MERGE_READINESS_IGNORE_CHECKS=policy/required-label,policy/title-format
```

You do not have to work this out yourself. When a check fails on nearly
every open pull request — the signature of a gate rather than a broken
build — the dashboard says so and gives you the exact line to paste. It
stops once configured.

---
