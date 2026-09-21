# Argus

A read-only dashboard for a GitHub organisation, and a sprint report for
Jira: what is waiting on your review, what is ready to merge, what has
gone stale, which security alerts matter, and where a sprint's capacity
actually went.

It runs as **one container on your own machine** and authenticates with
**your own** credentials, so everything is scoped to what you can already
see. Nothing is hosted and nothing is shared.

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
suite. The same holds for Jira: the client permits only a named list of
read endpoints, and refuses everything else.

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

## Quick start

You need **Docker or podman** — `run.sh` uses whichever it finds. You do
**not** need Go or Node; the image builds both, for whichever architecture
you are on.

```bash
cp .env.example .env     # set ARGUS_GITHUB_ORG
cp op.env.example op.env # or skip this and use `gh auth login`
./run.sh up
```

Open **<http://argus.localhost:18474>**. `*.localhost` resolves to
127.0.0.1 with no `/etc/hosts` entry and no `sudo`.

Argus binds to **loopback only**. It has no login and does not need one,
because anything that can reach it is already on your machine.

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
