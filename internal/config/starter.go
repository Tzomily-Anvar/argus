package config

// The starter configuration, compiled in so `argus init` needs no
// repository. It is deliberately short: the full reference lives in
// .env.example and the documentation, and a first-run file that opens
// with three hundred lines of options teaches nobody anything.
const starter = `# Argus configuration.
#
# Only the first setting is required. Everything else has a working
# default - uncomment a line only when the default is wrong for you.
#
# Full reference: https://github.com/Tzomily-Anvar/argus/blob/main/.env.example

# The GitHub organisation to sweep.
ARGUS_GITHUB_ORG=your-org-here

# How to authenticate. Pick one:
#
#   1. A GitHub App - no token to create, rotate, or authorise for SSO,
#      and read-only is enforced by GitHub rather than by Argus.
#      Nothing to set: just run  argus login
#
#      Uncomment only to use your own App instead of the published one.
# ARGUS_GITHUB_APP_CLIENT_ID=

#   2. A personal access token. See:
#      https://github.com/Tzomily-Anvar/argus/blob/main/docs/github-token.md
# ARGUS_GITHUB_TOKEN=

#   3. Nothing at all - Argus falls back to your ` + "`gh auth login`" + `.


# Which tools run. The pull request watchdog is the default; the sprint
# report needs Jira credentials and storage, so it is off unless asked for.
# ARGUS_TOOLS=pr

# The port. Deliberately unusual so it does not collide with a dev server,
# and http://argus.localhost:18474 works with no /etc/hosts entry.
# ARGUS_PORT=18474

# How often the background sweep runs. This is what makes the dashboard
# instant: you read the last result rather than waiting for a fresh one.
# ARGUS_REFRESH_MINUTES=15
`
