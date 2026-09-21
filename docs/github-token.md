# GitHub tokens

Argus authenticates with **your own** token, so everything it shows is
scoped to what you can already see. Nothing here is shared, and no token
is ever written to disk by Argus itself.

Either kind of token works. The differences are real but small:

| | Fine-grained | Classic |
|---|---|---|
| Read-only enforced by GitHub | **yes** | no (`repo` is read-write) |
| CI check status | via workflow runs | direct |
| Checks from other apps (CodeQL) | not visible | visible |
| Needs org-owner approval | **yes** | no |
| Maximum lifetime | 366 days | unlimited |

Whichever you pick, check it before wondering why something is missing:

```bash
./scripts/check-token.sh
```

That calls every endpoint Argus uses and names the permission behind any
failure. It checks by *result* rather than status code, because the most
confusing failure here returns `200` with an empty list rather than a
`403`.

## Creating the token

Argus needs a token that can read pull requests, teams, and (optionally)
security alerts across your organisation. Either kind works — the
difference is summarised here and detailed below.

| | Fine-grained | Classic |
|---|---|---|
| Read-only enforced by GitHub | **yes** | no (`repo` is read-write) |
| CI check status | via workflow runs | direct |
| Checks from other apps (CodeQL) | not visible | visible |
| Needs org-owner approval | **yes** | no |
| Maximum lifetime | 366 days | unlimited |

<details>
<summary><b>Fine-grained token — tighter, and it works</b></summary>

Fine-grained tokens are read-only at GitHub's level rather than by
convention, which is a real improvement over a classic token carrying
`repo`. Everything below is verified against a live organisation.

1. **[github.com/settings/personal-access-tokens](https://github.com/settings/personal-access-tokens)** →
   **Generate new token**.
2. **Resource owner: your organisation**, not your personal account. This
   is what makes team membership readable; without it the "waiting on
   your review" section silently misses anything assigned to a team.
3. **Repository access: All repositories.** Argus sweeps the whole
   organisation, so a selected subset makes it under-report with no error.
4. Grant these, all **read-only**:

   | Repository | Organization |
   |---|---|
   | Metadata *(automatic)* | **Members** |
   | Pull requests | |
   | Contents | |
   | Commit statuses | |
   | Issues *(pull request labels)* | |
   | Dependabot alerts *(optional)* | |
   | Code scanning alerts *(optional)* | |
   | Actions *(see below)* | |

5. **An organisation owner has to approve it.** Until they do, the token
   reads only public resources — so Argus will appear to work while
   returning almost nothing. Add a justification when you create it.

Check it before you wonder why something is missing:

```bash
./scripts/check-token.sh
```

That calls every endpoint Argus uses and names the permission behind any
failure. It checks by result rather than status code, because the most
confusing failure here returns `200` with an empty list rather than a
`403`.

### CI checks need `Actions`, not `Checks`

GitHub grants the **Checks API to GitHub Apps only** — there is no
`Checks` permission to tick, and a fine-grained token gets `403` on check
runs. Argus therefore falls back to **workflow runs**, a separate API
behind **Actions: Read**, which carries the conclusions of your own CI.

One trap worth knowing: **the two APIs name things differently.** A check
run is named for its job (`lint / lint`); a workflow run is named for its
workflow (`Lint`). If you list policy gates in
`ARGUS_RULE_MERGE_READINESS_IGNORE_CHECKS`, include **both spellings**, or
a gate will be counted as a real failure and every pull request will read
as broken.

What the fallback cannot see: check runs posted by *other* GitHub Apps,
such as CodeQL. Your own workflows are fully covered.

</details>

<details>
<summary><b>Classic token — simplest, and sees everything</b></summary>

A classic token reads the Checks API directly, so CI status needs no
fallback and third-party app checks are visible too. The cost is that
`repo` grants full read **and write**; only Argus's own enforcement stops
a write.

1. **[github.com/settings/tokens](https://github.com/settings/tokens)** →
   **Generate new token (classic)**.
2. Tick:

   | Scope | Why |
   |---|---|
   | `repo` | Pull requests, branches and checks in private repositories. |
   | `read:org` | Which teams you are on, for team review requests. |
   | `security_events` | *Optional.* Dependabot and code-scanning alerts. |

</details>

### If your organisation uses SAML single sign-on

The two token types differ here, and the classic one trips people up.

**Classic:** a valid token still has to be **authorised** for the
organisation separately, and this is the single most common reason a
correct-looking token fails.

> On [your tokens page](https://github.com/settings/tokens), find the
> token, click **Configure SSO**, and **Authorize** it for your
> organisation.

Without it you get `403` on every request. Argus recognises that specific
failure and says so rather than showing a generic error.

**Fine-grained:** there is no authorise-afterwards step. You may be asked
to sign in through SSO while creating the token, and that is all. What
replaces it is the organisation owner's approval — see the fine-grained
section above.

### A note on security alerts

Organisation-wide alerts need more than a permission: GitHub also
requires you to be an **organisation owner or security manager**. A plain
member gets `403` whichever token type they hold, and ticking the
permission does not change that.

If that is you, the security section shows an error and **everything else
still works** — or turn it off entirely:

```bash
ARGUS_RULE_SECURITY_ENABLED=false
```

## Giving Argus the token

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
