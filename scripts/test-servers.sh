#!/usr/bin/env bash
# Runs the whole Go test suite against a throwaway Postgres server in Docker.
# The container uses a random local port and a throwaway password, is removed
# on exit (even on failure), and never touches any other container.
set -uo pipefail

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "test-servers needs Docker, and it is not available. Start Docker and try again." >&2
  exit 1
fi

PW="waypoint-test-$RANDOM$RANDOM"
PG="waypoint-test-pg-$$"

cleanup() { docker rm -f "$PG" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

docker run --rm -d --name "$PG" -e POSTGRES_PASSWORD="$PW" -p 127.0.0.1::5432 postgres:17 >/dev/null || exit 1
PGPORT=$(docker port "$PG" 5432/tcp | head -1 | sed 's/.*://')

echo "waiting for Postgres on $PGPORT..."
for i in $(seq 1 90); do
  docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
# The official image restarts once during first-time setup; make sure it is over.
sleep 2
docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1 || { echo "Postgres did not become ready" >&2; exit 1; }

export WAYPOINT_TEST_POSTGRES="127.0.0.1:$PGPORT:postgres:$PW"
go test ./... -race -count=1 "$@"
