# Security

## Reporting a vulnerability

Please report security issues privately, through
[GitHub's private vulnerability reporting](https://github.com/Tzomily-Anvar/argus/security/advisories/new).

If that link is unavailable, open a normal issue asking for a private
channel and **leave the details out of it** - a public issue describing a
working exploit helps an attacker before it helps anyone else. A
maintainer will open a private advisory and reply there.

## Design

Argus is a local, read-only tool. A few properties are worth stating
explicitly, because they are what make it safe to point at an entire
organisation.

**It only reads.** Every request passes through a single function,
`assertReadOnly` in `internal/gh/client.go`, which permits `GET`, permits
`POST` only to GitHub's GraphQL endpoint, and there only for a document
beginning with `query`. A `mutation` is refused before it leaves the
process. This is asserted in `internal/gh/client_test.go`, so removing
the guarantee breaks the build.

**What it stores depends on the tools you enable.**

The pull request tool stores nothing: its snapshot lives in memory and is
gone when the container stops.

The sprint report stores what Jira has no source for — each person's
baseline capacity, how many days of a sprint they were away, and any note
explaining a difference between the two. That is personal data about
named colleagues, and it is treated as such:

- It never leaves the machine running Argus. There is no hosted component.
- It is excluded from version control, and a pre-push hook refuses to let
  it through.
- Absence is recorded only as **planned** or **unplanned**, never with a
  reason. That distinction carries the entire analytical signal — leave
  booked in advance should already be in the plan, absence that arose
  during the sprint explains a shortfall — while recording *why* someone
  was away would make this health data.
- Per-person records are deleted after three years by default
  (`ARGUS_SPRINT_RETAIN_YEARS`). The aggregates that trends are drawn from
  carry no personal data and are kept.

If you enable the sprint report, you are running a system that holds
personal data about your colleagues. Tell them.

**It holds no shared credential.** Nothing is distributed with Argus that
grants access to anything: no token in the image, no secret in the
repository, nothing written to a log. Whatever it authenticates with
belongs to the person running it, and there are two ways to supply that.

A personal access token is yours, supplied at start time from 1Password
or the GitHub CLI and injected into the process environment. Argus never
writes it to a file. The repository's `.gitignore` and `.dockerignore`
both exclude `.env` and `op.env`.

**The GitHub App path stores a session.** Signing in with `argus login`
puts it in the operating system's own secret store where there is one:
the login keychain on macOS, through `/usr/bin/security`, and the Secret
Service on Linux, through `secret-tool`, when both that tool and a
session bus are present. Everywhere else — Windows, and a headless Linux
box with no desktop session — it falls back to a file, as before:
`github-app-session.json` in Argus's data directory (`/data` in the
container, `~/Library/Application Support/argus` on macOS,
`~/.local/share/argus` on Linux), created with mode `0600` and replaced
atomically. Either way it holds a user-to-server access token (a `ghu_`
token, which GitHub expires after eight hours and Argus renews five
minutes before it lapses) and a refresh token, which lasts six months. A
session found in the file on a machine that has a keyring is moved into
the keyring on first read and the plaintext file deleted. `argus logout`
clears both. That session is the only credential Argus stores, and it is
one person's own.

**The keyring is not a sandbox.** What it buys is worth stating exactly,
because it is easy to overclaim: the session is encrypted at rest, is
unreadable while the keychain is locked, and is not picked up by anything
that copies or synchronises the configuration directory — a backup, a
dotfiles repository, a sync client. What it does not buy is isolation
from other processes running as you. On macOS any of them can read the
item back through `security` with no prompt, exactly as any of them could
read a `0600` file. Software running under your user account has your
session either way, and the keychain does not change that.

**On macOS the secret passes through a command line.** `security` cannot
read a password from standard input — given a bare `-w` it prompts on the
terminal and asks for the value twice — so Argus passes it as an
argument, where it is visible in `ps` for the life of one short process,
to processes running as this user. That costs nothing further: those same
processes can read the finished keychain item anyway. Linux's
`secret-tool` takes the secret on standard input, so nothing sensitive
reaches the process table there.

**There is no App private key.** A GitHub App's private key signs
server-to-server requests on an installation's behalf, and anyone holding
one can act as the App everywhere it is installed. Argus makes no such
request. It uses the user-to-server device flow, which needs neither a
private key nor a client secret — only a client id, which is public
information and ships as an ordinary default. So the published App
carries no shared secret for anyone to hold, leak, or have to rotate.
What running it does involve is set out below.

**It is scoped to you.** Argus can only see what your token can see. Two
people running it against the same organisation get different dashboards.
A user-to-server token is bounded twice over — by the App's own
permissions and by the access of the person who signed in — so it can
never reach anything that person could not already reach.

**The image has no shell.** The container is `distroless/static`: a
single static binary, no shell, no package manager, no interpreter. It
runs as a non-root user.

## The published App

`argus login` uses a published GitHub App, `argus-pull-requests`, whose
client id is compiled in as the default so that signing in works on a
fresh install with nothing configured. That App is owned by this
repository's author.

**Your token never reaches the App's owner.** The device flow issues it
from GitHub directly to the machine you ran `argus login` on. It passes
through no server belonging to the owner, because there is no such
server: Argus has no hosted component and no backend of any kind. The
owner cannot see, retrieve or use anyone's token.

**The token cannot exceed your own access.** It is bounded by the App's
permissions and by the access of the person who signed in, as above, so
installing the published App grants it nothing you do not already have.

**What the owner can see** is the list of accounts and organisations that
have installed the App, and the number of installations, on the App's
settings page. Not repository contents, not pull requests, not anyone's
data.

**What the owner could do** is generate a private key for the App and
mint installation tokens for any organisation that has installed it,
within the permissions that organisation granted. Argus itself makes no
server-to-server request and needs no private key, but ownership of an
App carries the ability to create one.

Two things bound that. The App requests read-only permissions, and
nothing beyond the list in [docs/github-app.md](docs/github-app.md).
And anyone who would rather not depend on an App somebody else owns can
run their own by setting `ARGUS_GITHUB_APP_CLIENT_ID`: the sign-in flow
is identical, only the App differs. See
[Running your own App](docs/github-app.md#running-your-own-app).

**An organisation can withhold approval.** An organisation may require an
owner to approve a third-party App before a member may use it against
organisation repositories. That approval is the organisation's own
control over everything above, and it is granted — or not — under
**Settings → Third-party Access**.

## Scope

Argus makes no outbound connection other than to `api.github.com`, over
HTTPS.

**It binds to loopback only.** The dashboard itself has no
authentication - it does not need any, because anyone who can reach it is
already on your machine. That property depends on the binding, so both
layers enforce it: the binary defaults to `127.0.0.1`, and Compose
publishes the port as `127.0.0.1:18474:18474` rather than the Docker
default, which would bind every interface and make your organisation's
data readable by anyone on your network.

If you deliberately change `ARGUS_BIND`, or drop the `127.0.0.1` prefix
from the port mapping, you are exposing an unauthenticated view of your
organisation - through your token - to everyone who can route to that
address. Do not do it without putting authentication in front.
