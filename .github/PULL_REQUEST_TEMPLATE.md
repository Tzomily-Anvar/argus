## What and why

<!-- What changes, and the problem it solves. If it changes what a
     number means or what a tool can write, say so plainly. -->

## How it was verified

<!-- The list from CONTRIBUTING.md. Tick what you ran; say what you
     could not. -->

- [ ] `gofmt -l .` prints nothing
- [ ] `go build ./...` and `go vet ./...`
- [ ] `go test ./...` and `go test -race ./...`
- [ ] `cd web && npm run build` (includes the type check), if the frontend changed
- [ ] `./scripts/private-check.sh --staged` reports nothing private

## Checklist

- [ ] No company-specific values were added: no organisation or repository names, project keys, label or status names, people, or internal URLs - in code, defaults, tests, fixtures or this description. Examples use `ABC-123`, `your-org` and `Person A`.
- [ ] Any new setting is documented in `.env.example`, with its default and what it does, and any new sprint or Jira setting has its entry in `docs/sprint-conventions.md`.
- [ ] Nothing new writes to GitHub, and any new Jira write is a new entry on the gate's closed list with its own body check and tests.
