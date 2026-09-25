#!/usr/bin/env bash
#
# Refuses to let private strings reach a public repository.
#
# The patterns themselves are NOT in this file, and must never be: a check
# that hardcoded "the company name we must not publish" would publish it.
# They live in .private-patterns, which is gitignored, and this script does
# nothing without it - which is the point. Anyone cloning this repository
# gets the mechanism and none of the contents.
#
#   ./scripts/private-check.sh              scan the commits about to be pushed
#   ./scripts/private-check.sh --all        scan the entire history
#   ./scripts/private-check.sh --staged     scan what is staged right now
#
# Checks file contents, file paths, commit messages and author identities,
# because a name can leak through any of them.
#
# Put --generic before any of those to scan for credentials only, reading
# no pattern file even if one is present. That is the mode CI runs in: a
# public runner has no pattern file and must never be given one, and
# saying so on the command line is clearer than relying on its absence.

set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

PATTERNS_FILE=".private-patterns"
RED=$'\033[31m'; GREEN=$'\033[32m'; DIM=$'\033[2m'; OFF=$'\033[0m'

# Credentials are checked whether or not this clone has been configured.
# Without a patterns file the check used to do nothing at all, which is
# the worst possible default for the one category of mistake that cannot
# be taken back.
#
# High-confidence patterns only: issued-token prefixes and key blocks,
# never a bare word like "password". A check that cries wolf is a check
# people learn to bypass.
BUILTIN=(
  'ghp_[A-Za-z0-9]{30,}'
  'gho_[A-Za-z0-9]{30,}'
  'ghu_[A-Za-z0-9]{30,}'
  'ghs_[A-Za-z0-9]{30,}'
  'ghr_[A-Za-z0-9]{30,}'
  'github_pat_[A-Za-z0-9_]{30,}'
  'sk-ant-[A-Za-z0-9_-]{30,}'
  'xox[baprs]-[A-Za-z0-9-]{12,}'
  'AKIA[0-9A-Z]{16}'
  'ASIA[0-9A-Z]{16}'
  'AIza[0-9A-Za-z_-]{35}'
  'glpat-[A-Za-z0-9_-]{20,}'
  'npm_[A-Za-z0-9]{36}'
  'dop_v1_[a-f0-9]{64}'
  'ATATT[A-Za-z0-9_=-]{20,}'
  'BEGIN [A-Z ]*PRIVATE KEY'
)
builtin_pattern=$(printf '%s|' "${BUILTIN[@]}")
builtin_pattern=${builtin_pattern%|}

# Patterns come from up to three places, all of them outside version
# control, and they are combined rather than overriding one another.
#
# The repository copy is gitignored, which is the point - anyone cloning
# this gets the mechanism and none of the contents. But it is also the
# weakness: it lives in exactly one directory, so a fresh clone, a
# worktree, or a colleague checking out the same repository silently
# falls back to credentials only and reports a clean run. Every guard
# that protects something confidential should fail loudly when it is not
# actually loaded, not quietly do less.
#
# So a per-user file is read as well. That one survives cloning, and on
# a machine where somebody works in several checkouts it is the copy
# that actually holds the line.
#
# --generic skips all of that on purpose. The message about a missing
# pattern file is for a person who may have forgotten theirs; on a CI
# runner nobody has forgotten anything, and a job that went looking for
# the team's patterns would be a job that one day gets given them.
generic=0
if [[ "${1:-}" == "--generic" ]]; then
  generic=1
  shift
fi

sources=()
[[ -n "${ARGUS_PRIVATE_PATTERNS:-}" ]] && sources+=("$ARGUS_PRIVATE_PATTERNS")
sources+=("${XDG_CONFIG_HOME:-$HOME/.config}/argus/private-patterns")
sources+=("$PATTERNS_FILE")

local_pattern=""
loaded_from=()
if [[ "$generic" -eq 0 ]]; then
  for src in "${sources[@]}"; do
    [[ -f "$src" ]] || continue
    part=$(grep -vE '^[[:space:]]*(#|$)' "$src" | paste -sd'|' -)
    [[ -z "$part" ]] && continue
    loaded_from+=("$src")
    if [[ -z "$local_pattern" ]]; then
      local_pattern="$part"
    else
      local_pattern="$local_pattern|$part"
    fi
  done
fi

if [[ "$generic" -eq 1 ]]; then
  echo "${DIM}private-check: credentials only, by request. No pattern file is read.${OFF}"
elif [[ ${#loaded_from[@]} -eq 0 ]]; then
  echo "${DIM}private-check: no pattern file, so only credentials are checked.${OFF}"
  echo "${DIM}  Looked in: ${sources[*]}${OFF}"
  echo "${DIM}  Copy .private-patterns.example to one of those to add your own.${OFF}"
fi

# What a hit is called at the end depends on what was loaded: blaming a
# pattern file that was never read would send someone looking in the
# wrong place.
if [[ -n "$local_pattern" ]]; then
  pattern="$builtin_pattern|$local_pattern"
  matched="$PATTERNS_FILE or a built-in credential pattern"
else
  pattern="$builtin_pattern"
  matched="a built-in credential pattern"
fi

mode="${1:-range}"
found=0

report() { found=1; printf '%s✗%s %s\n' "$RED" "$OFF" "$1"; }

scan_blob() {
  local obj="$1" where="$2" hits
  hits=$(git cat-file -p "$obj" 2>/dev/null | grep -niE "$pattern" | head -3)
  if [[ -n "$hits" ]]; then
    report "$where"
    sed 's/^/      /' <<<"$hits"
  fi
}

case "$mode" in
  --staged)
    echo "Checking staged changes..."
    while IFS= read -r file; do
      [[ -z "$file" ]] && continue
      hits=$(git show ":$file" 2>/dev/null | grep -niE "$pattern" | head -3)
      [[ -n "$hits" ]] && { report "staged: $file"; sed 's/^/      /' <<<"$hits"; }
      grep -qiE "$pattern" <<<"$file" && report "staged path: $file"
    done < <(git diff --cached --name-only --diff-filter=ACM)
    ;;

  --all)
    echo "Checking every object in history..."
    while IFS= read -r obj; do
      [[ "$(git cat-file -t "$obj" 2>/dev/null)" == blob ]] || continue
      scan_blob "$obj" "blob $obj"
    done < <(git rev-list --objects --all | cut -d' ' -f1 | sort -u)

    while IFS= read -r p; do report "path: $p"; done \
      < <(git rev-list --objects --all | cut -d' ' -f2- | grep -iE "$pattern" || true)

    while IFS= read -r l; do report "identity: $l"; done \
      < <(git log --all --format='%H %an <%ae> %cn <%ce>' | grep -iE "$pattern" || true)

    while IFS= read -r l; do report "commit message: $l"; done \
      < <(git log --all --format='%H%n%B' | grep -niE "$pattern" || true)
    ;;

  *)
    # Default: whatever is about to be pushed.
    #
    # git invokes a pre-push hook with two arguments (remote name and URL)
    # and feeds it ref pairs on stdin. That argument count is the only
    # reliable way to tell "running as a hook" from "run by hand" - testing
    # whether stdin is a terminal is not, because it is also not a terminal
    # when this is called from another script, where reading would block
    # forever waiting for input that never arrives.
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

    echo "Checking commits in $range..."
    commits=$(git rev-list "$range" 2>/dev/null)
    [[ -z "$commits" ]] && { printf '%s✓%s nothing to check\n' "$GREEN" "$OFF"; exit 0; }

    for c in $commits; do
      short=$(git rev-parse --short "$c")
      while IFS= read -r file; do
        [[ -z "$file" ]] && continue
        obj=$(git rev-parse "$c:$file" 2>/dev/null) || continue
        scan_blob "$obj" "$short $file"
        grep -qiE "$pattern" <<<"$file" && report "$short path: $file"
      done < <(git diff-tree --no-commit-id -r --name-only --diff-filter=ACM "$c" 2>/dev/null)
    done

    while IFS= read -r l; do report "message or identity: $l"; done \
      < <(git log --format='%H %an <%ae>%n%B' $range | grep -niE "$pattern" || true)
    ;;
esac

if [[ "$found" -ne 0 ]]; then
  echo
  echo "${RED}Refusing to continue.${OFF} Something matching $matched is present."
  echo "  Remove it, or if it is already committed, rewrite the history that contains it."
  exit 1
fi

printf '%s✓%s nothing private found\n' "$GREEN" "$OFF"
