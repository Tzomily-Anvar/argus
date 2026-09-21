#!/usr/bin/env bash
#
# Checks a GitHub token against everything Argus actually calls, and says
# which permission is missing when something fails.
#
# GitHub's own token screen cannot tell you this: it lists permissions,
# not whether the combination you picked covers the tool. Several failures
# here are also silent - an endpoint that returns 200 and an empty array
# rather than a 403 - so they are checked by result, not status code.
#
#   ./scripts/check-token.sh                      uses ARGUS_GITHUB_TOKEN or gh
#   ./scripts/check-token.sh <org>                override the organisation
#
# With 1Password:
#   op run --env-file=op.env -- ./scripts/check-token.sh

set -uo pipefail
cd "$(git rev-parse --show-toplevel 2>/dev/null || dirname "$0")"

GREEN=$'\033[32m'; RED=$'\033[31m'; YELLOW=$'\033[33m'; DIM=$'\033[2m'; OFF=$'\033[0m'

TOKEN="${ARGUS_GITHUB_TOKEN:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}"
if [[ -z "$TOKEN" ]] && command -v gh >/dev/null 2>&1; then
  TOKEN="$(gh auth token 2>/dev/null)"
fi
if [[ -z "$TOKEN" ]]; then
  echo "${RED}No token.${OFF} Set ARGUS_GITHUB_TOKEN, or run with: op run --env-file=op.env -- $0"
  exit 1
fi

ORG="${1:-${ARGUS_GITHUB_ORG:-}}"
if [[ -z "$ORG" ]] && [[ -f .env ]]; then
  ORG=$(grep -E '^ARGUS_GITHUB_ORG=' .env | tail -1 | cut -d= -f2- | tr -d '"')
fi
if [[ -z "$ORG" ]]; then
  echo "${RED}No organisation.${OFF} Pass one: $0 <org>"
  exit 1
fi

case "$TOKEN" in
  github_pat_*) KIND="fine-grained" ;;
  ghp_*)        KIND="classic" ;;
  gho_*)        KIND="gh CLI login" ;;
  ghs_*|ghu_*)  KIND="GitHub App" ;;
  *)            KIND="unrecognised" ;;
esac

echo "Checking a ${KIND} token against ${ORG}"
echo

api() { curl -s -w '\n%{http_code}' -H "Authorization: Bearer $TOKEN" \
        -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com$1"; }

fails=0
report() { # name, ok, detail, fix
  if [[ "$2" == "ok" ]]; then
    printf '  %s✓%s %-30s %s\n' "$GREEN" "$OFF" "$1" "$3"
  elif [[ "$2" == "warn" ]]; then
    printf '  %s!%s %-30s %s\n' "$YELLOW" "$OFF" "$1" "$3"
    [[ -n "${4:-}" ]] && printf '      %s%s%s\n' "$DIM" "$4" "$OFF"
  else
    fails=$((fails+1))
    printf '  %s✗%s %-30s %s\n' "$RED" "$OFF" "$1" "$3"
    [[ -n "${4:-}" ]] && printf '      %sneeds: %s%s\n' "$DIM" "$4" "$OFF"
  fi
}

# --- identity -------------------------------------------------------
out=$(api /user); code=$(tail -1 <<<"$out"); body=$(sed '$d' <<<"$out")
if [[ "$code" == 200 ]]; then
  ME=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('login',''))" <<<"$body")
  report "identity" ok "$ME"
else
  report "identity" fail "HTTP $code" "a valid, unexpired token"
  ME=""
fi

# --- teams: fails silently with 200 and an empty list ----------------
out=$(api /user/teams); code=$(tail -1 <<<"$out"); body=$(sed '$d' <<<"$out")
if [[ "$code" != 200 ]]; then
  report "teams" fail "HTTP $code" "Organization → Members: Read"
else
  n=$(python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" <<<"$body")
  if [[ "$n" -gt 0 ]]; then
    report "teams" ok "$n team(s)"
  else
    report "teams" fail "200 but empty" \
      "Organization → Members: Read, and the token's resource owner set to the org. Without this, reviews assigned to a team never appear."
  fi
fi

# --- search ----------------------------------------------------------
out=$(api "/search/issues?q=org:$ORG+is:pr+is:open&per_page=1")
code=$(tail -1 <<<"$out"); body=$(sed '$d' <<<"$out")
if [[ "$code" == 200 ]]; then
  n=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('total_count',0))" <<<"$body")
  if [[ "$n" -gt 0 ]]; then
    report "pull request search" ok "$n open"
  else
    report "pull request search" warn "0 results" \
      "Either there really are none, or repository access is 'selected' rather than all."
  fi
else
  report "pull request search" fail "HTTP $code" "Repository → Pull requests: Read, all repositories"
fi

# --- repositories -----------------------------------------------------
out=$(api "/orgs/$ORG/repos?per_page=1"); code=$(tail -1 <<<"$out")
[[ "$code" == 200 ]] \
  && report "repositories" ok "" \
  || report "repositories" fail "HTTP $code" "Repository → Metadata: Read"

# --- alerts -----------------------------------------------------------
out=$(api "/orgs/$ORG/dependabot/alerts?state=open&per_page=1"); code=$(tail -1 <<<"$out")
[[ "$code" == 200 ]] \
  && report "dependabot alerts" ok "" \
  || report "dependabot alerts" fail "HTTP $code" "Repository → Dependabot alerts: Read, AND you must be an org owner or security manager"

out=$(api "/orgs/$ORG/code-scanning/alerts?state=open&per_page=1"); code=$(tail -1 <<<"$out")
[[ "$code" == 200 ]] \
  && report "code scanning alerts" ok "" \
  || report "code scanning alerts" fail "HTTP $code" "Repository → Code scanning alerts: Read, AND org owner or security manager"

# --- GraphQL: check runs are the one thing fine-grained cannot do -----
repo=$(api "/orgs/$ORG/repos?per_page=1&sort=pushed" | sed '$d' \
       | python3 -c "import json,sys
d=json.load(sys.stdin)
print(d[0]['name'] if isinstance(d,list) and d else '')" 2>/dev/null)
if [[ -n "$repo" ]]; then
  q="{\"query\":\"query{repository(owner:\\\"$ORG\\\",name:\\\"$repo\\\"){defaultBranchRef{name}}}\"}"
  resp=$(curl -s -H "Authorization: Bearer $TOKEN" -X POST -d "$q" https://api.github.com/graphql)
  if grep -q '"defaultBranchRef"' <<<"$resp"; then
    report "graphql (branches)" ok ""
  else
    report "graphql (branches)" fail "no data" "Repository → Contents: Read"
  fi
fi

pr=$(api "/search/issues?q=org:$ORG+is:pr+is:open&per_page=1" | sed '$d' \
     | python3 -c "import json,sys
d=json.load(sys.stdin).get('items',[])
if d:
    u=d[0]['repository_url'].rsplit('/',1)[-1]
    print(u, d[0]['number'])" 2>/dev/null)
if [[ -n "$pr" ]]; then
  set -- $pr
  q="{\"query\":\"query{repository(owner:\\\"$ORG\\\",name:\\\"$1\\\"){pullRequest(number:$2){reviewDecision commits(last:1){nodes{commit{statusCheckRollup{contexts(first:5){nodes{__typename}}}}}}}}}\"}"
  resp=$(curl -s -H "Authorization: Bearer $TOKEN" -X POST -d "$q" https://api.github.com/graphql)
  if grep -q '"statusCheckRollup":{' <<<"$resp" && ! grep -q 'FORBIDDEN' <<<"$resp"; then
    report "graphql (check runs)" ok "CI check status available"
  elif grep -q '"reviewDecision"' <<<"$resp"; then
    report "graphql (check runs)" warn "review decision yes, check runs no" \
      "Expected on a fine-grained token: GitHub grants the Checks API only to GitHub Apps. Argus degrades rather than breaking."
  else
    report "graphql (pull requests)" fail "no data" "Repository → Pull requests: Read"
  fi
fi

echo
if [[ "$fails" -eq 0 ]]; then
  printf '%s✓%s everything Argus needs is available.\n' "$GREEN" "$OFF"
else
  printf '%s%d check(s) failed.%s Fix the permissions above and run this again.\n' "$RED" "$fails" "$OFF"
  exit 1
fi
