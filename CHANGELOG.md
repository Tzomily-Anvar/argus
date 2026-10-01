# Changelog

All notable changes to Argus are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet.

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

[Unreleased]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/Tzomily-Anvar/argus/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Tzomily-Anvar/argus/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Tzomily-Anvar/argus/releases/tag/v0.2.0
