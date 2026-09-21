# The GitHub App

Argus can sign in as a **GitHub App user** instead of carrying a personal
access token. It is the shorter path: there is nothing to create, nothing
to paste, nothing to rotate in a year's time, and no token to authorise
for SSO afterwards. Read-only stops being a property of Argus's own code
and becomes a property of the permissions the App was granted.

Everything it shows is still scoped to **you**. A user-to-server token
is bounded twice over — by the App's permissions, and by your own access
— so it can never reach anything you could not already reach yourself.

| | GitHub App | Personal access token |
|---|---|---|
| Anything to create or paste | **no** | yes |
| Read-only enforced by GitHub | **yes** | fine-grained only |
| Lifetime you have to manage | none | up to 366 days, then rotate |
| SAML SSO authorisation step | none | classic tokens need one |
| Needs org-owner approval | for organisation repositories | fine-grained only |
| Where the session is kept | the system keyring, or a `0600` file | nothing |

## Signing in

```bash
argus login
```

In the repository or against the container, `./run.sh login` does the
same thing. Nothing has to be configured first: the published Argus App's
client id is compiled in as the default. A client id is public
information — the device flow uses no client secret — so it ships as an
ordinary default rather than a credential.

The command prints a code and waits:

```

  Open https://github.com/login/device
  and enter this code:

      WDJB-MJHT

  Waiting...

```

Open that page in any browser, signed in as yourself, and enter the code.
GitHub asks you to authorise the App; when you do, the command completes:

```

  Signed in. The session is kept at the macOS keychain

```

It runs in the foreground on purpose. A device code expires after fifteen
minutes, and one printed by a detached container would sit unread in the
logs until it did.

`argus logout` forgets the session again.

## Whose App it is

The compiled-in default is a published App, `argus-pull-requests`, owned
by this repository's author. The device flow issues your token from
GitHub straight to your own machine: it passes through no server of the
owner's, there is no backend, and the owner cannot see or use anyone's
token. What the owner can see is which accounts and organisations have
installed the App — not repository contents, not anyone's data. As the
App's owner they could mint installation tokens for organisations that
have installed it, within the read-only permissions those organisations
granted; [SECURITY.md](../SECURITY.md#the-published-app) sets that out in
full. Run your own App instead if you would rather not depend on
somebody else's — see [Running your own App](#running-your-own-app).

## What it stores

A user access token and a refresh token, in the operating system's own
secret store where there is one: the login keychain on macOS, through
`/usr/bin/security`, and the Secret Service on Linux, through
`secret-tool`, when both that tool and a session bus are present.

Everywhere else — Windows, and a headless Linux box with no desktop
session — it falls back to a file, `github-app-session.json` in Argus's
data directory: `/data` in the container,
`~/Library/Application Support/argus` on macOS, `~/.local/share/argus`
on Linux, written with mode `0600`. A session left in that file on a
machine that has a keyring is moved into the keyring the next time it is
read, and the file is deleted. `argus logout` clears both.

The keyring gives you encryption at rest, a session that is unreadable
while the keychain is locked, and nothing in the directory a backup or a
sync client might pick up. It does not isolate the session from other
software running as you: on macOS anything running as your user can read
the item back through `security` without a prompt, exactly as it could
read a `0600` file.

[SECURITY.md](../SECURITY.md) states exactly what is in the session, why
it has to exist at all, what the keyring does and does not buy, and why
no App private key is involved anywhere.

## How long a session lasts

The access token lasts **eight hours**. Argus renews it five minutes
before it lapses, stores the new pair, and carries on — a sweep that
starts just before expiry does not fail midway, and you are not asked for
anything.

The refresh token lasts **six months**, and each renewal issues a fresh
one, so a tool in regular use never reaches that limit. Left untouched
over a long enough break it will, and the next run says so:

> not signed in to the GitHub App, or the session has lapsed.

Run `argus login` again. That is the only moment the App path asks
anything of you.

## Running your own App

Two reasons to want this: you would rather not depend on an App somebody
else owns, or your organisation will not approve a third-party App at
all. The sign-in flow is identical — only the App differs.

```bash
ARGUS_GITHUB_APP_CLIENT_ID=Iv1YourOwnClientId
```

Create it under **Settings → Developer settings → GitHub Apps → New
GitHub App** in your own account or organisation. What matters:

1. **Enable Device Flow.** Without it `argus login` has nothing to talk
   to. There is no callback URL to supply and no client secret to
   generate — Argus needs neither.
2. **Request read-only permissions**, and nothing beyond this list:

   | Repository *(read)* | Why |
   |---|---|
   | Metadata *(automatic)* | Listing the organisation's repositories |
   | Pull requests | Every pull request rule |
   | Contents | Branches, for the stale-branch rule |
   | Issues | Pull request labels |
   | Commit statuses | CI status on a commit |
   | Checks | Check runs — the one thing a token cannot read |
   | Actions *(optional)* | The workflow-run fallback, if you leave Checks out |
   | Dependabot alerts *(optional)* | The security section |
   | Code scanning alerts *(optional)* | The security section |

   | Organisation *(read)* | Why |
   |---|---|
   | Members | Which teams you are on, for team review requests |

   `Members` is not optional in practice: without it, review requests
   assigned to a team silently never appear.

3. **Install it on the organisation, on all repositories.** Authorising
   the App during sign-in is not the same as installing it. A partial
   installation makes Argus under-report with no error at all — the
   repositories left out simply are not there.

Then `argus login` as usual.

## If your organisation has to approve it

An organisation can require an owner to approve a GitHub App before a
member may use it against organisation repositories. Until that happens
you can sign in perfectly well and see almost nothing, which is the same
trap fine-grained tokens set.

If the dashboard comes up nearly empty straight after a successful
sign-in, this is the first thing to check. An owner approves the App
under **Settings → Third-party Access → GitHub Apps** in the
organisation, or installs your own App there.

## Troubleshooting

| What you see | What it means |
|---|---|
| `a GitHub App is configured but nobody has signed in` | Run `argus login`. Argus prefers the App whenever a client id is set, and one is set by default — so this appears even if you have a personal access token configured. |
| `not signed in to the GitHub App, or the session has lapsed` | The refresh token has passed six months. Sign in again. |
| `the code expired before it was entered` | The fifteen minutes ran out. Run the command again for a fresh code. |
| `authorisation was declined` | The App was refused in the browser, or the wrong account was signed in there. |
| Signed in, but the dashboard is nearly empty | The App is not installed on the organisation, is installed on a selected few repositories, or is waiting on an owner's approval. |
| "Waiting on your review" misses team requests | The App lacks **Organisation → Members: Read**. |
| The security section errors, everything else works | GitHub also requires you to be an organisation **owner or security manager** for organisation-wide alerts; the permission alone is not enough. Turn the section off with `ARGUS_RULE_SECURITY_ENABLED=false`. |

`argus doctor` reports whether a session exists before you go looking
anywhere else.
