#!/usr/bin/env bash
#
# Argus - start, stop and inspect the container.
#
#   ./run.sh up        build and start in the background   (default)
#   ./run.sh down      stop and remove the container
#   ./run.sh restart   rebuild and recreate, re-reading .env and the token
#   ./run.sh logs      follow the logs
#   ./run.sh status    show container state
#   ./run.sh login     sign in to the GitHub App, if one is configured
#   ./run.sh logout    forget that session
#   ./run.sh doctor    check the setup and explain anything missing
#   ./run.sh backup    copy the data volume to a dated tarball on the host
#   ./run.sh backups   list them, newest first
#   ./run.sh restore [file]   put one back (the newest if no file is given)
#
# up and restart take a backup first, so a rebuild never starts without one.
# Backups go to ~/.config/argus/backups (ARGUS_BACKUP_DIR), newest 30 kept
# (ARGUS_BACKUP_KEEP).
#
# The GitHub token is resolved at start time - from 1Password if you have
# an op.env, otherwise from your `gh` login - and injected straight into
# the container. It is never written to disk, baked into the image, or
# left in your shell history.

set -euo pipefail
cd "$(dirname "$0")"

# Re-entry point for `op run`, which needs an executable to hand the
# resolved environment to rather than a shell function.
if [[ "${1:-}" == "--compose" ]]; then
  shift
  ENGINE="${ARGUS_ENGINE:-}"
  if [[ -z "$ENGINE" ]]; then
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then ENGINE=docker
    elif command -v podman >/dev/null 2>&1; then ENGINE=podman; fi
  fi
  case "$ENGINE" in
    docker) exec docker compose "$@" ;;
    podman)
      if podman compose --help >/dev/null 2>&1; then exec podman compose "$@"; fi
      exec podman-compose "$@" ;;
    *) echo "no container engine found" >&2; exit 1 ;;
  esac
fi

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; DIM=$'\033[2m'; OFF=$'\033[0m'
ok()   { printf '%s✓%s %s\n' "$GREEN" "$OFF" "$1"; }
warn() { printf '%s!%s %s\n' "$YELLOW" "$OFF" "$1"; }
bad()  { printf '%s✗%s %s\n' "$RED" "$OFF" "$1"; }

port() { grep -E '^ARGUS_PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2 | tr -d '"' || true; }
PORT="$(port)"; PORT="${PORT:-18474}"

# Container engine. Docker if it is running, otherwise podman - which is a
# drop-in for everything used here and is what people reach for when Docker
# Desktop is unavailable or not licensed for their company.
detect_engine() {
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    echo docker
  elif command -v podman >/dev/null 2>&1; then
    echo podman
  fi
}
ENGINE="${ARGUS_ENGINE:-$(detect_engine)}"

# Compose, spelled for whichever engine is present. podman ships `podman
# compose` in recent versions and older installs use the separate
# podman-compose; both take the same arguments as `docker compose`.
compose() {
  case "$ENGINE" in
    docker) docker compose "$@" ;;
    podman)
      if podman compose --help >/dev/null 2>&1; then
        podman compose "$@"
      elif command -v podman-compose >/dev/null 2>&1; then
        podman-compose "$@"
      else
        bad "podman is installed but has no compose support."
        echo "  Install podman-compose, or set ARGUS_ENGINE=docker if you have Docker."
        exit 1
      fi
      ;;
    *)
      bad "No container engine found."
      echo "  Install Docker (docker.com/get-started) or podman (podman.io)."
      exit 1
      ;;
  esac
}

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

# Run compose, whichever engine is present, with the token resolved into
# the environment.
with_token() {
  if [[ -f op.env ]] && command -v op >/dev/null 2>&1; then
    op run --env-file=op.env -- "$0" --compose "$@"
  elif command -v gh >/dev/null 2>&1 && gh auth token >/dev/null 2>&1; then
    ARGUS_GITHUB_TOKEN="$(gh auth token)" compose "$@"
  else
    bad "No GitHub token available."
    echo "  Either:"
    echo "    - create op.env from op.env.example (1Password), or"
    echo "    - run 'gh auth login' and try again."
    echo "  See the token setup section of the README."
    exit 1
  fi
}

# Backups of the data volume. Everything Argus remembers - capacity, sprint
# snapshots, the write log, seen marks, chosen labels - lives on one volume,
# and a reset of the container engine takes it with it. These copy it out to
# the host as a dated tarball, through the container itself, so no other
# image is needed.
BACKUP_DIR="${ARGUS_BACKUP_DIR:-$HOME/.config/argus/backups}"
BACKUP_KEEP="${ARGUS_BACKUP_KEEP:-30}"
CONTAINER=argus

has_container() { "$ENGINE" container inspect "$CONTAINER" >/dev/null 2>&1; }

# backup [quiet] - writes $BACKUP_DIR/argus-data-<date>-<time>.tgz. A copy
# identical to the newest one is dropped, so starting twice in a row doesn't
# pile up duplicates; only the newest $BACKUP_KEEP are kept.
backup() {
  local quiet="${1:-}"
  if [[ -z "$ENGINE" ]] || ! has_container; then
    [[ -n "$quiet" ]] || bad "No $CONTAINER container to back up - start it first."
    [[ -n "$quiet" ]] || exit 1
    return 0
  fi
  ( umask 077; mkdir -p "$BACKUP_DIR" )
  local file tmp newest
  file="$BACKUP_DIR/argus-data-$(date +%Y-%m-%d-%H%M%S).tgz"
  tmp="$file.part"
  # gzip -n leaves the time out of the header, so unchanged data gives
  # identical bytes and the duplicate check below works.
  if ! ( umask 077; "$ENGINE" cp "$CONTAINER:/data" - | gzip -n > "$tmp" ); then
    rm -f "$tmp"
    bad "Backup failed; nothing was written."
    exit 1
  fi
  newest="$(ls -1t "$BACKUP_DIR"/argus-data-*.tgz 2>/dev/null | head -1 || true)"
  if [[ -n "$newest" ]] && cmp -s "$tmp" "$newest"; then
    rm -f "$tmp"
    [[ -n "$quiet" ]] || ok "No change since the last backup: $newest"
    return 0
  fi
  mv "$tmp" "$file"
  ls -1t "$BACKUP_DIR"/argus-data-*.tgz | tail -n +"$((BACKUP_KEEP + 1))" | while read -r old; do rm -f "$old"; done
  ok "Backed up the data to $file"
}

# restore <file> - copies a backup back into the volume, over what is there.
# Files that exist now but not in the backup are left alone.
restore() {
  local file="${1:-}"
  if [[ -z "$file" ]]; then
    file="$(ls -1t "$BACKUP_DIR"/argus-data-*.tgz 2>/dev/null | head -1 || true)"
    [[ -n "$file" ]] || { bad "No backups in $BACKUP_DIR."; exit 1; }
  fi
  [[ -f "$file" ]] || { bad "No such file: $file"; exit 1; }
  has_container || { bad "No $CONTAINER container - run ./run.sh up once, then restore."; exit 1; }
  echo "Restoring $file into the data volume."
  read -r -p "This overwrites the files it contains. Go on? [y/N] " yes
  [[ "$yes" == [yY]* ]] || exit 1
  backup quiet
  compose stop
  # A backup from this script holds data/..., as `cp` wrote it; an older
  # one taken from inside the volume holds the files themselves. -a keeps
  # their owner, the user the image runs as.
  local into="$CONTAINER:/data"
  if tar tzf "$file" | head -1 | grep -q '^data/'; then into="$CONTAINER:/"; fi
  gunzip -c "$file" | "$ENGINE" cp -a - "$into"
  compose start
  ok "Restored. Argus is starting on the restored data."
}

case "${1:-up}" in
  backup)
    backup
    ;;
  restore)
    restore "${2:-}"
    ;;
  backups)
    ls -1t "$BACKUP_DIR"/argus-data-*.tgz 2>/dev/null || echo "No backups in $BACKUP_DIR yet."
    ;;
  up|start)
    need_env
    backup quiet
    with_token up -d --build
    echo
    ok "Argus is running at http://localhost:${PORT}"
    echo "${DIM}  The first sweep takes a few seconds. After that it refreshes in the"
    echo "  background, so the dashboard is instant whenever you open it.${OFF}"
    echo "${DIM}  ./run.sh logs   watch it      ./run.sh down   stop it${OFF}"
    ;;
  restart)
    need_env
    backup quiet
    with_token up -d --build --force-recreate
    ok "recreated → http://localhost:${PORT}"
    ;;
  down|stop)
    compose down
    ;;
  logs)
    compose logs -f
    ;;
  status|ps)
    compose ps
    ;;
  login|logout)
    cmd="$1"
    # Runs on the host, in the foreground: a device code expires in
    # fifteen minutes, and one printed by a detached container would sit
    # unread in its logs until it did.
    need_env
    if ! command -v go >/dev/null 2>&1 && [[ ! -x ./argus ]]; then
      bad "Signing in needs the binary. Either install Go, or run:"
      echo "    $ENGINE run --rm -it --env-file .env -v argus_argus-data:/data argus -$cmd"
      exit 1
    fi
    set -a; . ./.env; set +a
    if [[ -x ./argus ]]; then ./argus "-$cmd"; else go run ./cmd/argus "-$cmd"; fi
    ;;
  doctor)
    echo "Argus setup check"
    echo
    case "$ENGINE" in
      docker) ok "using docker ($(docker --version 2>/dev/null | head -1))" ;;
      podman) ok "using podman ($(podman --version 2>/dev/null))" ;;
      *)      bad "no container engine found - install Docker or podman" ;;
    esac

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
    echo "usage: ./run.sh [up|down|restart|logs|status|doctor|login|logout|backup|backups|restore [file]]" >&2
    exit 1
    ;;
esac
