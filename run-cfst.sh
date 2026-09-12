#!/usr/bin/env sh
set -eu

APP_DIR="${APP_DIR:-/opt/dockerdata/cfst}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.yml}"
LOCK_FILE="${LOCK_FILE:-/tmp/cfst.lock}"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "Compose file not found: $COMPOSE_FILE" >&2
  exit 1
fi

# Avoid overlapping scheduled runs. Each test may take several minutes.
if command -v flock >/dev/null 2>&1; then
  exec flock -n "$LOCK_FILE" sh -c 'exec docker compose -f "$1" run --rm cfst' sh "$COMPOSE_FILE"
fi

exec docker compose -f "$COMPOSE_FILE" run --rm cfst
