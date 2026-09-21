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

**It holds no credential of its own.** The token is yours, supplied at
start time from 1Password or the GitHub CLI, and injected into the
process environment. It is never written to a file, never baked into the
image, and never logged. The repository's `.gitignore` and
`.dockerignore` both exclude `.env` and `op.env`.

**It is scoped to you.** Argus can only see what your token can see. Two
people running it against the same organisation get different dashboards.

**The image has no shell.** The container is `distroless/static`: a
single static binary, no shell, no package manager, no interpreter. It
runs as a non-root user.

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
