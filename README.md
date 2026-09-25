# Argus

A read-only dashboard for a GitHub organisation, and a sprint report for
Jira: what is waiting on your review, what is ready to merge, what has
gone stale, which security alerts matter, and where a sprint's capacity
actually went.

It runs **on your own machine** — as a single binary or as one container
— and authenticates with **your own** credentials, so everything is
scoped to what you can already see. Nothing is hosted and nothing is
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

**Argus never writes to GitHub.** It does not comment, approve, request
changes, label, merge, close, or open anything.

That is enforced rather than promised. Every request passes through one
function that permits `GET`, permits `POST` only to GitHub's GraphQL
endpoint, and only for a document beginning with `query` — a `mutation`
is refused before it is sent. [`internal/gh/client_test.go`](internal/gh/client_test.go)
asserts it, so a change that tried to make Argus write would fail the test
suite.

**The sprint report can write to Jira, and only when you switch it on.**
Off by default, it reads like the GitHub tool does. Set
`ARGUS_SPRINT_ALLOW_WRITES` and it can do a named list of things at
sprint close, and nothing else: set story points, set the assignee, and
add, correct or remove one worklog entry. Every change is previewed
first, approved individually by a person, checked against Jira again at
the moment of writing, and recorded in a local audit log from which it
can be reversed. The client's gate refuses anything not on that list
whether writes are on or off, and
[`internal/jira/client_test.go`](internal/jira/client_test.go) asserts
that too. Edits appear in Jira under your own account, because they are
made with your own token. See
[Closing out a sprint](docs/sprint-report.md#closing-out-a-sprint).

**What it stores depends on which tools you run.** The pull request tool
stores nothing at all — its snapshot lives in memory and is gone when the
container stops. The sprint report has to store what Jira has no source
for: each person's baseline, how much of a sprint they were away, and any
note explaining a delta.

That is personal data about named colleagues. It stays on the machine
running Argus, it is excluded from version control, and
[SECURITY.md](SECURITY.md) says exactly what is held, where, and for how
long.

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

The left rail switches between tools. At the bottom of it are four colour
themes — Slate, Glacier, Nightfall, Pine — each with its own light and
dark, remembered per browser.

---

## Installing

There are three ways in, in the order most people should try them. They
all end in the same place: a dashboard at
**<http://argus.localhost:18474>**, sweeping in the background so the
answer is already there when you look. `*.localhost` resolves to
127.0.0.1 with no `/etc/hosts` entry and no `sudo`.

Argus binds to **loopback only**. It has no login and does not need one,
because anything that can reach it is already on your machine.

### How it signs in

Worth settling first, because it is the part people get wrong.

The default is a **GitHub App sign-in**: run `argus login` and there is
nothing to configure beforehand — no token to create, no private key to
download. It uses the user-to-server device flow, which means there is no
client secret and no private key anywhere: not on your machine, not in
this repository. The token it gets back acts as *you*, so it can reach
exactly what you can already reach, and nothing else.

A **personal access token** still works if you would rather hold your own
credential. Set `ARGUS_GITHUB_TOKEN` and Argus uses that instead —
[docs/github-token.md](docs/github-token.md) has the exact permissions,
the SSO step, and a script that names whatever is missing.

With **neither**, Argus falls back to the token your existing
`gh auth login` already has, which is usually enough for a first look.

### 1. Homebrew — macOS

The shortest path, and the one to take unless you have a reason not to.

```bash
brew install Tzomily-Anvar/tap/argus
argus setup     # answers three questions and writes the configuration
argus           # run it
```

`argus setup` asks for your organisation, offers to sign you in, and
asks whether you want the sprint report. It checks the organisation
against GitHub while you are still there to correct it, and writes
`config.env` under your usual configuration directory — a
package-manager install has no repository to write into.

Signing in prints a short code and points you at
<https://github.com/login/device>. You type the code in, approve it, and
that is the whole of it.

If you would rather see every setting and choose for yourself, the long
way round does the same job:

```bash
argus init      # writes the annotated configuration file
argus login     # sign in to GitHub
argus doctor    # checks the setup and explains anything missing
```

`ARGUS_GITHUB_ORG` is the only required setting. Everything else has a
working default, and the file `argus init` writes documents all of it.

To have Argus start at login and keep sweeping in the background — so the
dashboard is already current the moment you open it, rather than starting
to fetch while you wait:

```bash
argus service install     # a launchd agent, per-user, no root
argus service uninstall   # undo it
```

### 2. Download the binary — Linux, Windows, or anyone not using Homebrew

Every release on the
[Releases page](https://github.com/Tzomily-Anvar/argus/releases) carries
builds for macOS and Linux (amd64 and arm64) and for Windows.

> **Windows is built but untested.** It compiles, and the code paths for
> it are written — `%AppData%` for configuration, Task Scheduler for
> starting at login — but nobody has yet run Argus on Windows. If you do,
> an issue saying whether it worked would be genuinely useful. The
> `systemd --user` unit on Linux is in the same position: written and
> reviewed, not yet run in anger. Download
the archive for your platform, extract it, and put `argus` somewhere on
your `PATH`. Then it is the same as above:

```bash
argus setup
argus
```

On **macOS**, a binary you downloaded is quarantined by Gatekeeper, which
refuses to run it and blames you rather than the quarantine. Clear it:

```bash
xattr -dr com.apple.quarantine ./argus
```

The Homebrew cask does that for you, which is one reason to prefer it.

On **Windows**, `argus service install` registers a Task Scheduler entry
that starts Argus at login; on **Linux** it writes a `systemd --user`
unit. Both are per-user and unprivileged — nothing here needs root.

Releases are built by GitHub Actions rather than on anyone's laptop, and
each one is published with a build provenance attestation. If you would
rather not take that on trust:

```bash
gh attestation verify argus_1.0.0_linux_amd64.tar.gz \
  --repo Tzomily-Anvar/argus
```

### 3. Docker

The right choice if you want Argus running continuously on a server, or
if you already live in Docker and would rather not have another binary on
your `PATH`.

You need **Docker or podman** — `run.sh` uses whichever it finds. You do
**not** need Go or Node; the image builds both, for whichever
architecture you are on.

```bash
cp .env.example .env     # set ARGUS_GITHUB_ORG
cp op.env.example op.env # or skip this and use `gh auth login`
./run.sh up
```

```bash
./run.sh doctor    # check the setup and explain anything missing
./run.sh logs      # follow the logs
./run.sh down      # stop it
```

**Stuck on the token?** → [docs/github-token.md](docs/github-token.md),
or run `./scripts/check-token.sh`, which names the missing permission
rather than leaving you to guess.

---

## Documentation

| | |
|---|---|
| [**GitHub tokens**](docs/github-token.md) | Fine-grained or classic, exact permissions, SSO, and the 1Password setup |
| [**The sprint report**](docs/sprint-report.md) | Enabling it, what it computes, and the conventions you configure |
| [**Rules**](docs/rules.md) | Every check, how to turn one off or retune it, and which repositories are swept |
| [**Contributing**](CONTRIBUTING.md) | Adding a rule — one file, no wiring |
| [**Security**](SECURITY.md) | What is enforced, what is stored, and for how long |
| [`.env.example`](.env.example) | Every setting, with its default and what it does |

---

## Configuration

Everything is an environment variable in `.env`, and nothing carries a
company-specific default. The two that matter most:

```bash
ARGUS_GITHUB_ORG=your-org    # required; Argus refuses to start without it
ARGUS_TOOLS=pr               # pr, or pr,sprint
```

The pull request watchdog is the default and the only tool on unless you
ask for more — everything else costs credentials, storage, or both.

Rules are independent and individually tunable:

```bash
ARGUS_RULE_STALE_PRS_ENABLED=false   # opt out entirely
ARGUS_RULE_STALE_PRS_DAYS=21         # or just retune it
```

The **Rules** button in the dashboard lists every check live, with the
exact variable for each knob. A test fails the build if a setting exists
without being documented in `.env.example`, so that reference cannot drift
from the code.

---

## Troubleshooting

| What you see | What it means |
|---|---|
| `ARGUS_GITHUB_ORG is required` | No `config.env` or `.env`, or it still says `your-org-here`. |
| `GitHub rejected the token (401)` | Token missing, mistyped, expired, or revoked. |
| `403 with a SAML/SSO error` | Token is valid but not authorised for the org — see [SSO](#if-your-organisation-uses-saml-single-sign-on). |
| `403` on the security section only | Token cannot read org-wide alerts. Add `security_events`, or set `ARGUS_RULE_SECURITY_ENABLED=false`. |
| `404` for the org | `ARGUS_GITHUB_ORG` is wrong, or your token cannot see it. |
| Empty sections, no error | Genuinely nothing to show. The Rules panel confirms which checks ran. |
| Port already in use | Set `ARGUS_PORT` in your configuration file. |

`argus doctor` checks all of the above and says which step failed, as
does `./run.sh doctor` on the Docker path.

---

## Updating

However you installed it, your settings and your session are kept outside
the thing being replaced, so an update never costs you either.

Homebrew:

```bash
brew upgrade argus
```

If you installed the background service, run `argus service uninstall`
and `argus service install` afterwards: the unit points at the exact
binary it was written for, and an upgrade moves that.

A downloaded binary: take the new archive from the
[Releases page](https://github.com/Tzomily-Anvar/argus/releases) and
replace the one on your `PATH`. Your `config.env` is in your
configuration directory, not next to the binary, so it stays where it is.

Docker:

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
