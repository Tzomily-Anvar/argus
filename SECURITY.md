# Security

## Reporting a vulnerability

Please report security issues privately through
[GitHub's private vulnerability reporting](https://github.com/Tzomily-Anvar/argus/security/advisories/new)
rather than opening a public issue.

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

**It stores nothing.** There is no database and no state written to disk.
The snapshot lives in memory for the lifetime of the container.

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
