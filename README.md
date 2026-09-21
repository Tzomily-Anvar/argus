# Argus

A read-only dashboard for a GitHub organisation: what is waiting on your
review, what nobody has picked up, what is approved and ready to merge,
what has gone stale, and which security alerts are worth your attention.

It runs as **one container on your own machine** and authenticates with
**your own** GitHub token, so the view is automatically scoped to you —
your review requests, your teams, your branches, and only the
repositories you can already see. Nothing is hosted and nothing is
shared.

It sweeps in the background and serves from memory, so the dashboard is
**instant whenever you open it**, however slow the underlying API was.

---

## Why this exists

Hello! Argus was made to automate part of my work, and has tools necessary
for running my team the way I need. Some of the tools have to do with PR
statuses and points of attention for me, other tools have to do with
reporting on Sprints and handling the backlog. As a Lead Engineer in a
startup my needs are various, and when I noticed how much time could be
saved by making tools for these jobs instead of using AI for them, it felt
like a journey worth taking. This repo is also a learning place for me, to
try new things and test tools, languages, etc. Feel free to use it, leave
remarks or suggestions.

And if you did end up here, I hope Argus the all-seeing sees what you need
him to see — or, if you like the Odyssey, Argus is your loyal hound
waiting for you to go on your next hunt.

---

## What it does, and what it will never do

Argus only ever **reads**. It does not comment, approve, request changes,
label, merge, close, or open anything.

That is enforced rather than promised. Every request passes through one
function that permits `GET`, permits `POST` only to GitHub's GraphQL
endpoint, and only for a document beginning with `query` — a `mutation`
is refused before it is sent. [`internal/gh/client_test.go`](internal/gh/client_test.go)
asserts this, so a change that tried to make Argus write would fail the
test suite.

It also stores nothing. There is no database and no state on disk; the
snapshot lives in the container's memory and is gone when it stops.

---

## What you see

A summary row across the top — what is waiting on you, what is ready to
merge, what nobody has claimed — then one section per question:

| Section | Answers |
|---|---|
| **Overview** | Everything at once, ordered by how much it is your move. Sections with nothing in them collapse into a single "all clear" line. |
| **To review** | Other people's pull requests waiting on *you*. |
| **Ready to merge** | Approved, green, nothing left to do. |
| **Ready to QA** | Approved and green, only the QA label outstanding. Appears only if you configure one. |
| **Unclaimed** | Review-ready, but nobody has been assigned. |
| **Yours** | Your own open pull requests, drafts included. |
| **All open** | The full inventory, with review and check status. |
| **Stale** | Pull requests and branches that have stopped moving. |
| **Security** | Dependabot and code-scanning alerts, vendored noise discounted. |

The left rail switches between tools. There is one today; it is a rail
rather than a tab strip because the next ones drop in beside it. At the
bottom of it are four colour themes — Slate, Glacier, Nightfall, Pine —
each with its own light and dark, remembered per browser.

## Setup

Three steps: a token, a config file, and `./run.sh up`. Budget ten
minutes the first time.

You need **Docker or podman** — `run.sh` uses whichever it finds, and
`ARGUS_ENGINE` forces one if you have both. You do **not** need Go, Node,
or anything else; the image builds both.

The image is built for whatever architecture you are on, so it runs
natively on an Apple Silicon Mac and on an x86 server from the same
Dockerfile, with no emulation.

### 1. Create a GitHub token

Argus needs a token that can read pull requests, teams, and (optionally)
security alerts across your organisation.

<details open>
<summary><b>Classic token — simpler, recommended to start</b></summary>

1. Go to **[github.com/settings/tokens](https://github.com/settings/tokens)** →
   **Generate new token** → **Generate new token (classic)**.
2. Name it something you will recognise later, e.g. `argus`.
3. Set an expiry. 90 days is a reasonable balance; you will get an email
   before it lapses.
4. Tick these scopes:

   | Scope | Why |
   |---|---|
   | `repo` | Read pull requests, branches and checks in private repositories. |
   | `read:org` | Resolve which teams you belong to, for team review requests. |
   | `security_events` | *Optional.* Dependabot and code-scanning alerts. |

5. **Generate token** and copy it. GitHub shows it exactly once.

</details>

<details>
<summary><b>Fine-grained token — tighter, more setup</b></summary>

Fine-grained tokens grant narrower access, but organisation-owned ones
usually need an admin to approve the request before they work.

1. **[github.com/settings/personal-access-tokens](https://github.com/settings/personal-access-tokens)** →
   **Generate new token**.
2. Set **Resource owner** to your organisation. If this triggers an
   approval request, an org admin has to approve it before Argus works.
3. **Repository access:** All repositories (Argus sweeps org-wide).
4. Grant **read-only** on:

   | Permission | Type |
   |---|---|
   | Contents | Repository |
   | Metadata | Repository |
   | Pull requests | Repository |
   | Members | Organisation |
   | Dependabot alerts | Organisation *(optional)* |
   | Code scanning alerts | Organisation *(optional)* |

</details>

#### If your organisation uses SAML single sign-on

This is the single most common reason a correct-looking token fails. A
valid token still has to be **authorised** for the organisation
separately:

> On [your tokens page](https://github.com/settings/tokens), find the
> token, click **Configure SSO**, and **Authorize** it for your
> organisation.

Without this you get `403` on every request. Argus detects that specific
failure and tells you so rather than showing a generic error.

#### A note on security alerts

The security rule needs permission to read organisation-wide alerts,
which many organisations restrict to owners and security managers. If
you do not have it, that one section shows an error and **everything
else still works** — or turn it off entirely:

```bash
ARGUS_RULE_SECURITY_ENABLED=false
```

### 2. Give Argus the token

Pick whichever you already use. Either way the token is injected straight
into the container at start time — it is never written to disk, baked
into the image, or left in your shell history.

<details open>
<summary><b>Option A — 1Password (recommended)</b></summary>

Keeps the token in your vault. You never paste it anywhere, and rotating
it means editing the item, not the config.

1. **Install the CLI** and turn on desktop integration:

   ```bash
   brew install 1password-cli     # macOS; see 1password.com/downloads/command-line for others
   ```

   Then in the 1Password app: **Settings → Developer → Integrate with
   1Password CLI**. This is what lets `op` unlock with Touch ID instead of
   asking for your password.

2. **Store the token.** In *your own private vault*, create a new item:

   - Type: **API Credential**
   - Title: **GitHub Argus Token**
   - In the **credential** field: paste the token from step 1

3. **Point Argus at it:**

   ```bash
   cp op.env.example op.env
   ```

   The default reference is `op://Private/GitHub Argus Token/credential`.
   If your vault or item is named differently, edit `op.env` to match — the
   format is `op://<vault>/<item>/<field>`.

4. Check it resolves:

   ```bash
   ./run.sh doctor
   ```

> Use **your own** item in **your own** vault. Argus scopes everything to
> whoever the token belongs to, so pointing the whole team at one shared
> item would show everyone the same person's dashboard.

</details>

<details>
<summary><b>Option B — the GitHub CLI</b></summary>

If you already use `gh`, there is nothing to set up:

```bash
gh auth login
```

`run.sh` falls back to `gh auth token` when there is no `op.env`.

One caveat: `gh`'s own token carries the scopes `gh` asked for, which may
not include `security_events`. If the security section errors, create a
dedicated token via Option A instead.

</details>

### 3. Configure and start

```bash
cp .env.example .env
$EDITOR .env          # set ARGUS_GITHUB_ORG to your organisation
./run.sh up
```

Argus listens on **loopback only**, so the dashboard is reachable from
your machine and nowhere else. It has no login, and it does not need one
for that reason — see [SECURITY.md](SECURITY.md) before changing it.

Open **<http://argus.localhost:18474>** — or `localhost:18474`, whichever
you prefer. `*.localhost` resolves to 127.0.0.1 in browsers and modern
operating systems with no `/etc/hosts` entry and no `sudo`.

Want it without the port? Set `ARGUS_PORT=80` in `.env` and it becomes
plain **<http://argus.localhost>**. Port 80 is shared, though, so
anything else that wants it later will clash.

The first sweep takes a few seconds. After that the container keeps
sweeping in the background, so every subsequent visit is instant. Leave
it running — that is what the unusual port is for.

```bash
./run.sh up        # build and start in the background (default)
./run.sh logs      # follow the logs
./run.sh status    # container state
./run.sh doctor    # check the setup and explain anything missing
./run.sh restart   # rebuild, re-read .env, refresh the token
./run.sh down      # stop it
```

---

## The rules

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
[`.env.example`](.env.example), which is generated from the rules
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

## Troubleshooting

| What you see | What it means |
|---|---|
| `ARGUS_GITHUB_ORG is required` | No `.env`, or it still says `your-org-here`. |
| `GitHub rejected the token (401)` | Token missing, mistyped, expired, or revoked. |
| `403 with a SAML/SSO error` | Token is valid but not authorised for the org — see [SSO](#if-your-organisation-uses-saml-single-sign-on). |
| `403` on the security section only | Token cannot read org-wide alerts. Add `security_events`, or set `ARGUS_RULE_SECURITY_ENABLED=false`. |
| `404` for the org | `ARGUS_GITHUB_ORG` is wrong, or your token cannot see it. |
| Empty sections, no error | Genuinely nothing to show. The Rules panel confirms which checks ran. |
| Port already in use | Set `ARGUS_PORT` in `.env`. |

`./run.sh doctor` checks all of the above and says which step failed.

---

## Updating

```bash
git pull
./run.sh restart
```

Your `.env` and `op.env` are gitignored, so they survive updates.

---

## Development

```bash
go test ./...                                   # includes the read-only guarantee
go build -o argus ./cmd/argus                   # API only, without the UI
cd web && npm install && npm run build          # build the dashboard
```

For frontend work, run the binary and use Vite's dev server — it proxies
`/api` through, so the UI hot-reloads without rebuilding Go:

```bash
ARGUS_GITHUB_ORG=your-org ARGUS_GITHUB_TOKEN=$(gh auth token) ./argus &
cd web && npm run dev
```

Adding a rule means adding one file — see [CONTRIBUTING.md](CONTRIBUTING.md).

## How this was built

Argus was written with [Claude Code](https://claude.com/claude-code)
doing most of the typing, and the direction, review and decisions coming
from a person.

That division is worth stating plainly, because it is visible in the
result. The architecture calls — Go over Python, no database until
something needs one, a background sweep rather than sweeping on page
load — were human decisions, argued for and sometimes argued against.
So were several corrections that an assistant working alone would have
shipped: a section labelled "Ready to merge" that listed every open pull
request, a summary panel that vanished as you navigated, and a tab called
"On you" that most readers took to mean the opposite of what it did.

Every line was read before it was committed. If you find something wrong
here, it is the author's to answer for.

## Licence

[MIT](LICENSE).
