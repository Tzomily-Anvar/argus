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

## Setup

Three steps: a token, a config file, and `./run.sh up`. Budget ten
minutes the first time.

You need [Docker](https://docs.docker.com/get-started/get-docker/). You
do **not** need Go, Node, or anything else — the image builds both.

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

Open **<http://localhost:18474>**.

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
tunable without touching code. The **Rules** tab in the dashboard lists
every one of them live, with the exact environment variable for each
knob.

| Rule | What it surfaces |
|---|---|
| `review_requested` | Open PRs where a review is requested from you or your teams. |
| `unreviewed` | Review-ready PRs nobody has been assigned to. |
| `merge_readiness` | Every open PR with its review decision and check status. |
| `my_prs` | Your own open PRs, drafts included. |
| `stale_prs` | PRs older than a threshold, with bot PRs counted separately. |
| `stale_branches` | Branches with no commit in a long time. |
| `security` | Dependabot and code-scanning alerts, plus open Dependabot PRs. |

Turn one off, or retune it, in `.env`:

```bash
ARGUS_RULE_STALE_PRS_ENABLED=false      # opt out entirely
ARGUS_RULE_STALE_PRS_DAYS=21            # or just retune it
```

Then `./run.sh restart`. Every knob and its default is documented in
[`.env.example`](.env.example), which is generated from the rules
themselves and so cannot drift from them.

### Real failures versus policy gates

Many organisations have a required check that is a *policy* rather than a
test — "this PR is missing a required label", say. It fails red exactly
like a broken build, which makes approved, working PRs look broken.

Tell Argus which checks are policy gates and it reports them separately,
so `real_failures` means something is actually wrong:

```bash
ARGUS_RULE_MERGE_READINESS_IGNORE_CHECKS=policy/required-label,policy/title-format
```

---

## Troubleshooting

| What you see | What it means |
|---|---|
| `ARGUS_GITHUB_ORG is required` | No `.env`, or it still says `your-org-here`. |
| `GitHub rejected the token (401)` | Token missing, mistyped, expired, or revoked. |
| `403 with a SAML/SSO error` | Token is valid but not authorised for the org — see [SSO](#if-your-organisation-uses-saml-single-sign-on). |
| `403` on the security section only | Token cannot read org-wide alerts. Add `security_events`, or set `ARGUS_RULE_SECURITY_ENABLED=false`. |
| `404` for the org | `ARGUS_GITHUB_ORG` is wrong, or your token cannot see it. |
| Empty sections, no error | Genuinely nothing to show. The counts in the Rules tab confirm the sweep ran. |
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

## Licence

[MIT](LICENSE).
