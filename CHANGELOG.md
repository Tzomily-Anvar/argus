# Changelog

All notable changes to Argus are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Every pull request says where it stands with you: `you approved`,
  `you commented` or `you asked for changes` while your latest review is
  on the code as it stands, and **changed since your review** once
  someone pushes after it. Read from GitHub by the sweep, comments and
  thread replies included.
- *Mark seen*, on hover, for a pull request you read without leaving a
  review. The mark is kept in Argus with the commit you were looking at,
  never sent to GitHub, and a push brings the pull request back as
  changed since you looked.
- A **Looked at** filter on every pull request tab - not yet, seen,
  reviewed, changed since, yours - kept in the address like the others.
  Not offered on Yours.

### Changed

- A pull request counts as approved once it has as many approvals as
  the rulesets on its repository's default branch ask for. GitHub judges
  against the branch a pull request merges into, so one stacked on a
  feature branch the rulesets miss was "approved" on a single review;
  it now reads "1/2 approvals" and stays out of Ready to merge. The
  review badge shows the count (`approved 2/2`, `needs review 1/2`), and
  a pull request that merges into a branch other than the default says
  which. Where no ruleset asks for a count, or none can be read,
  GitHub's decision stands as before.

## [0.3.5] - 2026-10-28

### Added

- The Backlog's label pickers list every label, not only the ones
  chosen behind the gear: the chosen ones first, then the repositories
  pushed to recently, by name, then every label the open backlog
  carries and the rest of the site's labels. A label nobody has used
  can be typed and created in the same picker, and one typed that is
  another spelling of an existing label is offered as that one instead.
- `ARGUS_BACKLOG_LABEL_STYLE` names how the team spells a label of
  several words - kebab, snake, camel, pascal or train (`Argus-API`).
  New labels are suggested in it, and the Labels view lists every
  label spelt otherwise, and every label spelt more than one way, with
  the migration that settles it. The labels chosen behind the gear are
  exempt, for a deliberate exception such as `DevOps`.
- `ARGUS_BACKLOG_REPO_LABEL_DAYS` (90) sets how recently a repository
  must have been pushed to for its name to be offered as a label.
- The pull request tables and the Backlog's ticket tables sort by
  clicking a column heading. A pull request's status sorts by how much
  it needs someone: failing first, approved last.
- A filter bar above them: search, plus author and repository for pull
  requests, and type, status, reporter, assignee and label for tickets.
  It stays on across tabs, and each table says how many of its rows it
  shows. Sort and filter live in the address, so a reload or a pasted
  link keeps them. While a Backlog filter is on, a section's buttons act
  on the rows it shows only. Only the filters that can narrow a table
  are offered on it: no author on Yours, and a picker with one value
  to choose is greyed out.

### Changed

- *Remove label* and *Migrate label* on the bulk bar list the labels on
  the selected tickets, with how many of them carry each, instead of
  the chosen list.
- The Groups view names its empty groups together on one line instead
  of giving each a card of disabled buttons, so the page is as long as
  the work in it.
- Closing a Backlog picker with Escape returns focus to its button.

## [0.3.4] - 2026-10-22

### Fixed

- The Backlog's searchable pickers open in full. The list was drawn
  inside its card, which clipped it, so a picker at a card's right edge
  - the Labels view's "Pick a label", say - showed one entry and pushed
  the card sideways when its search box took focus. The list now floats
  above the page, keeps to the screen and follows its button on scroll.

## [0.3.3] - 2026-10-21

### Fixed

- `brew install --cask argus` on Linux. The cask's old postflight ran
  `/usr/bin/xattr`, which Linux does not have, and the install failed.
  The declared step below skips quietly where there is no `xattr`.

### Changed

- The Homebrew cask clears macOS's quarantine flag with declared
  `postflight_steps` instead of a Ruby `postflight` block, which
  Homebrew 7 deprecates and warns about on every `brew info` and
  install. Needs Homebrew 5.1.14 or later, which `brew update` brings.
- Dependencies updated: the dashboard builds with Vite 8 (Rolldown in
  place of esbuild and Rollup), TypeScript 7, `@vitejs/plugin-react` 6
  and React 19.3.1; goose 3.29, `golang.org/x/text` and `x/sync` on the
  Go side. Nothing changes in behaviour.

## [0.3.2] - 2026-10-21

### Security

- Binaries and the container image are built with Go 1.26.10. v0.3.1 was
  built with Go 1.26.0, whose standard library has since had fixes in
  `net/http`, `crypto/tls`, `crypto/x509`, `html/template`, `net/url` and
  others. Argus talks to GitHub and Jira over TLS and serves its own
  HTTP, so these apply. Upgrade by installing this release; nothing in
  configuration or stored data changes.
- The dashboard's build tools are updated past advisories in `vite` and
  `source-map-js`. These affect only the development server, never the
  released binary.
- CI runs `govulncheck`, so a toolchain or module with a published fix
  fails the build.

## [0.3.1] - 2026-10-01

### Added

- A **Drafts** tab on the pull request dashboard. Drafts are read like
  every other pull request and shown apart, with a count, so a
  repository whose only open pull requests are drafts no longer looks
  like one with nothing open. `ARGUS_RULE_MERGE_READINESS_EXCLUDE_DRAFTS=false`
  folds them back into All open.

### Changed

- The section tabs are one row that scrolls sideways when the page is
  narrow, instead of wrapping.

## [0.3.0] - 2026-10-01

### Added

- **The Backlog tool** (`ARGUS_TOOLS=pr,sprint,backlog`). What in the
  open backlog needs a hand: new tickets until you acknowledge them,
  stale ones, work with no Epic, no Story, no labels or no size, Epics
  with unrefined work beneath them, and requests from outside the team -
  found by a configurable label, by legacy spellings of it, or by a
  roster of requesters kept in the app and importable from an Atlassian
  team. A personal inbox of mentions, assignments and watched changes
  across Jira and Confluence. Acknowledging and dismissing write nothing
  to Jira. See `docs/backlog.md`.
- **Bulk actions on the Backlog**, behind the same `ARGUS_SPRINT_ALLOW_WRITES`
  switch and the same closed-list gate as the sprint close-out: assign
  to a sprint, set the Epic, link to a Story, add or remove labels,
  replace a legacy label, and add the request label. Each is previewed
  per ticket, re-checked at write time, logged, and reversible from the
  log. Deleting an issue sits behind a second switch,
  `ARGUS_BACKLOG_ALLOW_DELETE`, off by default, with a typed count and
  no reverse.
- **A labels workflow.** The labels the bulk bar offers are chosen
  behind the gear, from what the open backlog carries or typed fresh;
  `ARGUS_BACKLOG_WORK_LABELS` names labels that mean a kind of
  engineering work, so a request from outside the team that carries one
  is flagged.
- **The review Leaderboard.** Code reviews per person, this fortnight
  and last, tallied from the pull request sweep with self-reviews and
  bots left out. On whenever the `pr` tool is; a thank-you to the people
  carrying review, not a target.
- **An "unresolved" badge** on pull requests with review threads nobody
  has resolved.
- **Linux packages.** Each release carries a `.deb`, an `.rpm` and an
  `.apk` for amd64 and arm64 beside the archives, attested like them.
  The package holds the binary and the docs and nothing else;
  `argus service install` writes the `systemd --user` unit on request.
- The sprint report reads what Jira declares - status categories, the
  board's estimation field - and catalogues every convention it still
  has to ask about, on `docs/sprint-conventions.md`, with a test holding
  the code to the page.
- The Epic picker offers open Epics from other projects named in
  `ARGUS_BACKLOG_EPIC_PROJECTS`.
- Community files: a code of conduct, issue forms, a pull request
  template and this changelog.

### Changed

- `ARGUS_GITHUB_ORG` is forgiving: paste the organisation's page address
  and the account name is read out of it, wherever the setting is read.
  A value that is still not a GitHub login is refused with the rule
  spelled out, and `argus doctor` says what it read.
- The default for `ARGUS_JIRA_STORY_LINK_TYPES` is now `Blocks,Relates`,
  the two link types every Jira site declares. A team whose history
  holds a legacy link type names it in the setting.
- The shipped default for `ARGUS_BACKLOG_LEGACY_LABELS` is empty; older
  spellings of the request label are a team's own to name.

## [0.2.0] and earlier

Everything up to and including 0.2.0 - the pull request dashboard, the
GitHub App sign-in, the sprint report, the Jira write gate and the
sprint close-out, Confluence publishing, the Homebrew tap and the
release pipeline - is described on the
[GitHub Releases page](https://github.com/Tzomily-Anvar/argus/releases),
which carries the notes for each tag.

[Unreleased]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.5...HEAD
[0.3.5]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Tzomily-Anvar/argus/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Tzomily-Anvar/argus/releases/tag/v0.2.0
