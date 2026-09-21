# Contributing

## Adding a rule

A rule is one file. It declares what it is, what it can be tuned by, and
how to produce its rows; it registers itself, so there is no central list
to edit and nothing to wire up.

Create `internal/rules/your_rule.go`:

```go
package rules

func init() {
	Register(Rule{
		ID:          "big_prs",
		Title:       "Unusually large pull requests",
		Description: "Open pull requests touching more files than the threshold.",
		Why:         "Large PRs get reviewed slowly and badly. Splitting them up is usually faster overall.",
		Enabled:     true,
		Params: []Param{
			{Name: "max_files", Desc: "Flag pull requests touching more files than this.", Default: 40},
		},
		Run: runBigPRs,
	})
}

func runBigPRs(c *Context, v Values) (any, error) {
	items, err := c.Search("is:pr is:open")
	if err != nil {
		return nil, err
	}
	limit := v.Int("max_files")

	rows := gh.PMap(items, c.Concurrency, func(it map[string]any) map[string]any {
		pr, err := c.Client.Get("/repos/"+c.Org+"/"+RepoName(it)+"/pulls/"+NumberStr(it), nil)
		if err != nil || int(gh.Num(pr["changed_files"])) <= limit {
			return nil
		}
		return Row(it, c.Now, map[string]any{"changed_files": gh.Num(pr["changed_files"])})
	})

	return SortByAge(Compact(rows)), nil
}
```

That is the whole job. You now automatically get:

- `ARGUS_RULE_BIG_PRS_ENABLED` and `ARGUS_RULE_BIG_PRS_MAX_FILES`
- an entry in `GET /api/rules` and in the dashboard's **Rules** tab
- inclusion in the background sweep
- a block in `.env.example` next time it is regenerated

### What the helpers give you

| Helper | Use |
|---|---|
| `c.Search(q)` | Issue search, already scoped to the organisation. |
| `c.Client.Get` / `GetAll` / `GraphQL` | Raw read-only API access. `GetAll` follows pagination. |
| `gh.PMap` | Concurrent fan-out, order preserved. |
| `Row(item, now, extra)` | The shared row shape the frontend renders. |
| `SortByAge`, `Compact`, `Rows` | Ordering, dropping nils, and never returning JSON `null`. |
| `gh.Map` / `List` / `Str` / `Num` / `Bool` | Nil-safe JSON walking. |

### Rules to follow

- **Read only.** `assertReadOnly` will refuse a write, and the test suite
  will fail. If you need a write, this is the wrong project.
- **Write `Why` for the reader**, not the maintainer. It appears in the
  UI as the explanation of why the rule is worth acting on.
- **Make thresholds parameters**, never constants. Someone else's
  organisation is not shaped like yours.
- **Return `[]`, never `nil`.** Use the helpers; `Rows()` exists for this.
- **Keep organisation-specific values out of defaults.** A default should
  make sense to a stranger. Label names, check names and repository
  conventions belong in `.env`, not in the code.

### Before opening a pull request

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./...
```

`registry_test.go` checks that every rule has a title, a description, a
`Why`, uniquely named parameters, and supported parameter types — so a
rule missing its help text fails the build rather than shipping blank.

## Regenerating `.env.example`

The per-rule section is generated from the running registry, so it cannot
drift from the code:

```bash
./run.sh up
curl -fsS localhost:18474/api/rules > /tmp/rules.json
# then regenerate the rule blocks from that JSON
```

## Keeping private strings out

This repository is public and was extracted from an internal one, so
there is a check that refuses to let private strings reach it:

```bash
git config core.hooksPath .githooks              # once per clone
cp .private-patterns.example .private-patterns   # optional, see below
```

That installs two hooks. **pre-commit** scans what you have staged, so a
mistake is still just an unstage; **pre-push** scans the commits about to
leave, as a last line of defence.

**Credentials are checked whether or not you configure anything.** Issued
token prefixes and private key blocks are built into the script, because
a clone that protects nothing is the worst possible default for the one
category of mistake that cannot be taken back.

`.private-patterns` adds *your own* strings on top - an employer name,
internal hosts, identifier formats, colleagues' names. It is gitignored,
and deliberately so: a committed check listing "the name we must not
publish" would publish it.

From then on every `git push` is scanned first, and it refuses if anything
matches - in file contents, file paths, commit messages or author
identities, because a name can leak through any of them.

```bash
./scripts/private-check.sh           # what is about to be pushed
./scripts/private-check.sh --all     # the entire history
./scripts/private-check.sh --staged  # what is staged right now
```

Without a `.private-patterns` file the check does nothing and says so, so
a clone without one is not silently unprotected - it is visibly
unconfigured.

## Frontend

```bash
cd web && npm install && npm run dev
```

Vite proxies `/api` to a locally running binary, so the UI hot-reloads
without rebuilding Go.

Two conventions worth keeping:

- **Status colour never carries meaning alone.** Every badge pairs its
  colour with a glyph and a written label — two of the four status
  colours sit below 3:1 contrast on the light surface by design, and the
  pairing is the mitigation.
- **Dark mode is a chosen set of steps**, validated against the dark
  surface, not an automatic inversion of the light ones.
