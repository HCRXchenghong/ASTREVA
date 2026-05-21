#!/usr/bin/env sh
set -eu

HTTP_BASE="${HTTP_BASE:-http://localhost:8088}"
WS_BASE="${WS_BASE:-ws://localhost:8088}"
DURATION="${DURATION:-60s}"
INTERVAL="${INTERVAL:-1s}"
MESSAGES_PER_CONN="${MESSAGES_PER_CONN:-3}"
STEPS="${STEPS:-100 500 1000 2500 5000}"

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"

for n in $STEPS; do
  echo "== wsload n=$n duration=$DURATION interval=$INTERVAL messages_per_conn=$MESSAGES_PER_CONN =="
  (cd "$ROOT_DIR/backend" && go run ./cmd/wsload \
    -http "$HTTP_BASE" \
    -ws "$WS_BASE" \
    -n "$n" \
    -duration "$DURATION" \
    -interval "$INTERVAL" \
    -messages-per-conn "$MESSAGES_PER_CONN")
done
