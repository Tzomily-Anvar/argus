#!/usr/bin/env bash
#
# Lists every author and committer address in this repository and says which
# ones are not allowed.
#
# The hooks guard what you create locally. This guards everything else: a
# commit made before the hooks existed, one cherry-picked in from a clone
# configured differently, and above all a squash or merge performed in the
# GitHub web interface, which is created on GitHub's servers under the
# address on the account and which no local hook ever sees.
#
# Run it in any clone, from any directory inside it, whenever you want to
# know. It changes nothing and exits non-zero if anything is off the
# allowlist, so it also works as a scheduled or CI check.
#
#   ./scripts/check-authors.sh
#
# The allowlist lives in git config; scripts/identity-check.sh documents it,
# along with the account settings that close the web-interface hole.

set -uo pipefail

# The allowlist and email_allowed() come from the guard itself, so there is
# one definition of "allowed" and not two that can drift apart.
. "$(git rev-parse --show-toplevel)/scripts/identity-check.sh"

echo "Allowed in this repository:"
for a in "${ALLOWED[@]}"; do echo "    $a"; done
echo
echo "Every identity on every ref, most used first:"
echo

if [[ -z "$(git rev-list -n1 --all 2>/dev/null)" ]]; then
  echo "${DIM}  no commits yet${OFF}"
  exit 0
fi

bad=0
while IFS='|' read -r count email; do
  [[ -z "$email" ]] && continue
  if email_allowed "$email"; then
    printf '  %s✓%s %6d  %s\n' "$GREEN" "$OFF" "$count" "$email"
  else
    bad=$((bad+1))
    printf '  %s✗%s %6d  %s\n' "$RED" "$OFF" "$count" "$email"
  fi
done < <(git log --all --format='%ae%n%ce' 2>/dev/null \
         | sort | uniq -c | sort -rn \
         | sed 's/^ *\([0-9][0-9]*\) \(.*\)$/\1|\2/')

if [[ "$bad" -eq 0 ]]; then
  echo
  printf '%s✓%s every identity is on the allowlist\n' "$GREEN" "$OFF"
  exit 0
fi

echo
echo "${RED}$bad address(es) not on the allowlist.${OFF} They appear in:"
while IFS='|' read -r sha ae ce subject; do
  [[ -z "$sha" ]] && continue
  if ! email_allowed "$ae" || ! email_allowed "$ce"; then
    printf '      %s  author %s  committer %s\n' "$sha" "$ae" "$ce"
    printf '        %s%s%s\n' "$DIM" "$subject" "$OFF"
  fi
done < <(git log --all --format='%h|%ae|%ce|%s' 2>/dev/null | head -200)

echo
echo "  Set the right identity in the clone that produced them:"
echo "      git config user.email ${ALLOWED[0]}"
echo "  A commit already in history can only be changed by rewriting it"
echo "  (git commit --amend --reset-author for the tip, a rebase or a history"
echo "  rewrite for anything older), and every sha after it changes with it."
echo
echo "  If an address is genuinely yours, allow it rather than weaken the check."
echo "  Configuring any address replaces the list above, so name them all:"
echo "      git config --add argus.allowed-email <address>   (once per address)"
echo
echo "${DIM}  If one of these came from a merge made in the GitHub web interface,${OFF}"
echo "${DIM}  no local hook can prevent the next one. Turn on, in your account:${OFF}"
echo "${DIM}    Settings -> Emails -> Keep my email address private${OFF}"
echo "${DIM}    Settings -> Emails -> Block command line pushes that expose my email${OFF}"
exit 1
