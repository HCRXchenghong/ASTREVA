#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
HTTP_BASE="${HTTP_BASE:-http://localhost:8088}"
WS_BASE="${WS_BASE:-ws://localhost:8088}"
DURATION="${DURATION:-60s}"
INTERVAL="${INTERVAL:-1s}"
MESSAGES_PER_CONN="${MESSAGES_PER_CONN:-3}"
STEPS="${STEPS:-100 500 1000 2500 5000}"

echo "HTTP_BASE=$HTTP_BASE"
echo "WS_BASE=$WS_BASE"
echo "DURATION=$DURATION"
echo "INTERVAL=$INTERVAL"
echo "MESSAGES_PER_CONN=$MESSAGES_PER_CONN"
echo "STEPS=$STEPS"

for n in $STEPS; do
  echo "== acceptance wsload n=$n =="
  output="$(
    cd "$ROOT_DIR/backend"
    go run ./cmd/wsload \
      -http "$HTTP_BASE" \
      -ws "$WS_BASE" \
      -n "$n" \
      -duration "$DURATION" \
      -interval "$INTERVAL" \
      -messages-per-conn "$MESSAGES_PER_CONN"
  )"
  echo "$output"
  failed="$(echo "$output" | sed -n 's/.*failed=\([0-9][0-9]*\).*/\1/p')"
  connected="$(echo "$output" | sed -n 's/.*connected=\([0-9][0-9]*\).*/\1/p')"
  if [[ "${failed:-0}" != "0" ]]; then
    echo "failed connections detected at n=$n" >&2
    exit 1
  fi
  if [[ "${connected:-0}" != "$n" ]]; then
    echo "connected=$connected does not match target=$n" >&2
    exit 1
  fi
done

echo "acceptance load test passed"
