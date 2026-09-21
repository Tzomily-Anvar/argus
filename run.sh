#!/usr/bin/env bash
#
# Argus - start, stop and inspect the container.
#
#   ./run.sh up        build and start in the background   (default)
#   ./run.sh down      stop and remove the container
#   ./run.sh restart   rebuild and recreate, re-reading .env and the token
#   ./run.sh logs      follow the logs
#   ./run.sh status    show container state
#   ./run.sh doctor    check the setup and explain anything missing
#
# The GitHub token is resolved at start time - from 1Password if you have
# an op.env, otherwise from your `gh` login - and injected straight into
# the container. It is never written to disk, baked into the image, or
# left in your shell history.

set -euo pipefail
cd "$(dirname "$0")"

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; DIM=$'\033[2m'; OFF=$'\033[0m'
ok()   { printf '%s✓%s %s\n' "$GREEN" "$OFF" "$1"; }
warn() { printf '%s!%s %s\n' "$YELLOW" "$OFF" "$1"; }
bad()  { printf '%s✗%s %s\n' "$RED" "$OFF" "$1"; }

port() { grep -E '^ARGUS_PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2 | tr -d '"' || true; }
PORT="$(port)"; PORT="${PORT:-18474}"

need_env() {
  if [[ ! -f .env ]]; then
    bad "No .env found."
    echo "  Create one and set your organisation:"
    echo "      cp .env.example .env"
    echo "      \$EDITOR .env        # set ARGUS_GITHUB_ORG"
    exit 1
  fi
  if grep -qE '^ARGUS_GITHUB_ORG=your-org-here' .env; then
    bad "ARGUS_GITHUB_ORG is still the placeholder in .env."
    echo "  Set it to your GitHub organisation, then run this again."
    exit 1
  fi
}

# Run docker compose with the token resolved into the environment.
with_token() {
  if [[ -f op.env ]] && command -v op >/dev/null 2>&1; then
    op run --env-file=op.env -- docker compose "$@"
  elif command -v gh >/dev/null 2>&1 && gh auth token >/dev/null 2>&1; then
    ARGUS_GITHUB_TOKEN="$(gh auth token)" docker compose "$@"
  else
    bad "No GitHub token available."
    echo "  Either:"
    echo "    - create op.env from op.env.example (1Password), or"
    echo "    - run 'gh auth login' and try again."
    echo "  See the token setup section of the README."
    exit 1
  fi
}

case "${1:-up}" in
  up|start)
    need_env
    with_token up -d --build
    echo
    ok "Argus is running at http://localhost:${PORT}"
    echo "${DIM}  The first sweep takes a few seconds. After that it refreshes in the"
    echo "  background, so the dashboard is instant whenever you open it.${OFF}"
    echo "${DIM}  ./run.sh logs   watch it      ./run.sh down   stop it${OFF}"
    ;;
  restart)
    need_env
    with_token up -d --build --force-recreate
    ok "recreated → http://localhost:${PORT}"
    ;;
  down|stop)
    docker compose down
    ;;
  logs)
    docker compose logs -f
    ;;
  status|ps)
    docker compose ps
    ;;
  doctor)
    echo "Argus setup check"
    echo
    command -v docker >/dev/null 2>&1 && ok "docker installed" || bad "docker not found - install Docker Desktop"
    docker info >/dev/null 2>&1 && ok "docker daemon running" || bad "docker daemon not running - start Docker Desktop"

    if [[ -f .env ]]; then
      if grep -qE '^ARGUS_GITHUB_ORG=your-org-here' .env; then
        bad ".env exists but ARGUS_GITHUB_ORG is still the placeholder"
      else
        ok ".env present, organisation set to $(grep -E '^ARGUS_GITHUB_ORG=' .env | tail -1 | cut -d= -f2)"
      fi
    else
      bad "no .env - run: cp .env.example .env"
    fi

    if [[ -f op.env ]]; then
      if command -v op >/dev/null 2>&1; then
        if op run --env-file=op.env -- sh -c 'test -n "$ARGUS_GITHUB_TOKEN"' 2>/dev/null; then
          ok "1Password reference resolves"
        else
          bad "op.env exists but the reference does not resolve - check the vault, item and field names"
        fi
      else
        warn "op.env exists but the 1Password CLI is not installed - will fall back to gh"
      fi
    elif command -v gh >/dev/null 2>&1 && gh auth token >/dev/null 2>&1; then
      ok "gh is logged in; its token will be used"
    else
      bad "no token source - create op.env, or run 'gh auth login'"
    fi

    if curl -fsS "http://localhost:${PORT}/healthz" >/dev/null 2>&1; then
      ok "Argus is answering on http://localhost:${PORT}"
    else
      warn "nothing answering on port ${PORT} yet (fine if you have not started it)"
    fi
    ;;
  *)
    echo "usage: ./run.sh [up|down|restart|logs|status|doctor]" >&2
    exit 1
    ;;
esac
