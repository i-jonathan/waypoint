#!/usr/bin/env bash
# Runs the whole Go test suite against throwaway Postgres and MySQL servers in
# Docker. Containers use random local ports and a throwaway password, are
# removed on exit (even on failure), and never touch any other container.
set -uo pipefail

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "test-servers needs Docker, and it is not available. Start Docker and try again." >&2
  exit 1
fi

PW="waypoint-test-$RANDOM$RANDOM"
SUFFIX="$$"
PG="waypoint-test-pg-$SUFFIX"
MY="waypoint-test-my-$SUFFIX"

cleanup() { docker rm -f "$PG" "$MY" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

docker run --rm -d --name "$PG" -e POSTGRES_PASSWORD="$PW" -p 127.0.0.1::5432 postgres:17 >/dev/null || exit 1
docker run --rm -d --name "$MY" -e MYSQL_ROOT_PASSWORD="$PW" -p 127.0.0.1::3306 mysql:8.4 >/dev/null || exit 1

port() { docker port "$1" "$2/tcp" | head -1 | sed 's/.*://'; }
PGPORT=$(port "$PG" 5432)
MYPORT=$(port "$MY" 3306)

echo "waiting for Postgres on $PGPORT and MySQL on $MYPORT..."
for i in $(seq 1 90); do
  docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
for i in $(seq 1 120); do
  docker exec "$MY" mysql -uroot -p"$PW" -e 'SELECT 1' >/dev/null 2>&1 && break
  sleep 1
done
# The official images restart once during first-time setup; make sure it is over.
sleep 2
docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1 || { echo "Postgres did not become ready" >&2; exit 1; }
docker exec "$MY" mysql -uroot -p"$PW" -e 'SELECT 1' >/dev/null 2>&1 || { echo "MySQL did not become ready" >&2; exit 1; }

export WAYPOINT_TEST_POSTGRES="127.0.0.1:$PGPORT:postgres:$PW"
export WAYPOINT_TEST_MYSQL="127.0.0.1:$MYPORT:root:$PW"
go test ./... -race -count=1 "$@"
