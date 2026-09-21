#!/usr/bin/env bash
#
# Refuses to let the wrong identity reach a public repository.
#
# Nothing here is secret - the allowed address is public by design. This is
# about a mistake that is easy to make and expensive to take back: one clone
# configured with one email, another clone with a different one, and a commit
# published under whichever happened to be in scope at the time. Once it is
# pushed, the only way to remove it is to rewrite history.
#
# The allowlist is configuration rather than a rule baked into this file,
# because one person legitimately has more than one address:
#
#   git config --add argus.allowed-email you@example.com   (repeatable)
#
# With none configured the default below applies. Configuring even one
# address replaces that default outright rather than adding to it, so list
# every address you use - that is what lets this drop into a repository
# which has nothing to do with this one. A list of forbidden domains would
# be the wrong shape: it only ever catches the addresses somebody thought
# of in advance.
#
#   ./scripts/identity-check.sh --config    the identity a commit would get now
#   ./scripts/identity-check.sh             every commit about to be pushed
#   ./scripts/identity-check.sh --all       every commit on every ref
#
# WHAT THIS CANNOT DO
#
# A hook only runs where git runs. A squash, merge or edit performed in the
# GitHub web interface is created on GitHub's servers, stamped with the
# address configured on the account, and no local hook of any kind is
# consulted - that is exactly how a wrong address gets published even though
# every local commit was clean. That hole is closed in the account settings,
# not here:
#
#   Settings -> Emails -> Keep my email address private
#   Settings -> Emails -> Block command line pushes that expose my email
#
# Turn both on. This script covers locally created commits only. Use
# ./scripts/check-authors.sh to audit what is already in the history.

set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1

RED=$'\033[31m'; GREEN=$'\033[32m'; DIM=$'\033[2m'; OFF=$'\033[0m'

# Only reached when nothing is configured. Kept to the addresses that are
# already all over this repository's history and are public on purpose.
DEFAULT_ALLOWED=(
  '69584443+Tzomily-Anvar@users.noreply.github.com'
  'noreply@github.com'
)

lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

# Held as written, so the addresses this prints back are the ones to type.
ALLOWED=()
while IFS= read -r _entry; do
  [[ -z "$_entry" ]] && continue
  ALLOWED+=("$_entry")
done < <(git config --get-all argus.allowed-email 2>/dev/null || true)
if [[ "${#ALLOWED[@]}" -eq 0 ]]; then
  ALLOWED=("${DEFAULT_ALLOWED[@]}")
fi

# Compared case-insensitively: git preserves the case you typed, GitHub
# does not care about it, and neither should this.
email_allowed() {
  local want a
  want=$(lower "${1:-}")
  [[ -z "$want" ]] && return 1
  for a in "${ALLOWED[@]}"; do
    [[ "$want" == "$(lower "$a")" ]] && return 0
  done
  return 1
}

ident_email() { sed -n 's/.*<\(.*\)>.*/\1/p' <<<"${1:-}"; }

found=0
report() { found=1; printf '%s✗%s %s\n' "$RED" "$OFF" "$1"; }

advice() {
  local a first="${ALLOWED[0]}"
  echo
  echo "${RED}Refusing to continue.${OFF} That address is not allowed in this repository."
  echo "  allowed here:"
  for a in "${ALLOWED[@]}"; do echo "      $a"; done
  echo
  echo "  Point this clone at the right identity:"
  echo "      git config user.email $first"
  echo "  Repair the commit that already carries the wrong one:"
  echo "      git commit --amend --reset-author"
  echo "  Older than the tip: rebase onto its parent and amend it there, or"
  echo "  re-create the branch. Either way every sha after it changes."
  echo
  echo "  If the address is genuinely yours, allow it rather than weaken the check."
  echo "  Configuring any address replaces the list above, so name them all:"
  echo "      git config --add argus.allowed-email <address>   (once per address)"
  echo
  echo "${DIM}  A merge made in the GitHub web interface cannot be caught here.${OFF}"
  echo "${DIM}  See WHAT THIS CANNOT DO at the top of scripts/identity-check.sh.${OFF}"
}

# Sourced by scripts/check-authors.sh, which wants the allowlist and
# email_allowed() and nothing else. Only run the checks when executed.
[[ "${BASH_SOURCE[0]}" != "${0}" ]] && return 0

mode="${1:-range}"

check_commits() { # a git log range, or --all
  local sha ae ce short
  while IFS='|' read -r sha ae ce; do
    [[ -z "$sha" ]] && continue
    short="${sha:0:7}"
    email_allowed "$ae" || report "$short author: $ae"
    if [[ "$ce" != "$ae" ]]; then
      email_allowed "$ce" || report "$short committer: $ce"
    fi
  done < <(git log --format='%H|%ae|%ce' "$@" 2>/dev/null)
}

case "$mode" in
  --config)
    # Before a commit exists, not after. user.email is what people actually
    # get wrong; git var is what the commit would really be stamped with,
    # including GIT_AUTHOR_EMAIL and GIT_COMMITTER_EMAIL from the
    # environment, which user.email does not show.
    echo "Checking the identity this commit would carry..."
    cfg=$(git config --get user.email 2>/dev/null || true)
    if [[ -z "$cfg" ]]; then
      report "no user.email is set for this repository"
      advice
      exit 1
    fi

    seen=""
    check_one() { # label, address
      local label="$1" val="${2:-}"
      [[ -z "$val" ]] && return 0
      email_allowed "$val" && return 0
      case "$seen" in *"<$val>"*) return 0 ;; esac
      seen="$seen<$val>"
      report "$label: $val"
    }

    check_one "user.email" "$cfg"
    check_one "author email" "$(ident_email "$(git var GIT_AUTHOR_IDENT 2>/dev/null)")"
    check_one "committer email" "$(ident_email "$(git var GIT_COMMITTER_IDENT 2>/dev/null)")"
    ;;

  --all)
    echo "Checking the identity of every commit on every ref..."
    check_commits --all
    ;;

  *)
    # Every commit being pushed, not just the tip: a push can carry commits
    # made before this hook existed, or cherry-picked in from a clone that
    # was configured differently.
    #
    # The range is derived exactly as scripts/private-check.sh derives it,
    # and for the same reason: git invokes a pre-push hook with two
    # arguments (remote name and URL) and feeds it ref pairs on stdin, and
    # that argument count is the only reliable way to tell "running as a
    # hook" from "run by hand". Testing whether stdin is a terminal is not,
    # because it is also not a terminal when this is called from another
    # script, where reading would block forever.
    range=""
    if [[ $# -ge 2 ]]; then
      while read -r _local_ref local_sha _remote_ref remote_sha; do
        [[ "$local_sha" =~ ^0+$ ]] && continue
        if [[ "$remote_sha" =~ ^0+$ ]]; then
          range="$local_sha"
        else
          range="$remote_sha..$local_sha"
        fi
      done
    fi
    [[ -z "$range" ]] && range="origin/main..HEAD"
    git rev-parse --verify --quiet "${range%%..*}" >/dev/null 2>&1 || range="HEAD"

    echo "Checking identities in $range..."
    if [[ -z "$(git rev-list "$range" 2>/dev/null)" ]]; then
      printf '%s✓%s nothing to check\n' "$GREEN" "$OFF"
      exit 0
    fi
    check_commits "$range"
    ;;
esac

if [[ "$found" -ne 0 ]]; then
  advice
  exit 1
fi

printf '%s✓%s identity allowed\n' "$GREEN" "$OFF"
