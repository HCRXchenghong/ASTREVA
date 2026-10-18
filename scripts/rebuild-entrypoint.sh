#!/bin/sh
set -eu

if [ -n "${PUBLISH_DIR:-}" ] && [ ! -f "$PUBLISH_DIR/current/index.html" ]; then
  echo "[rebuild] 首次启动，正在生成官网静态文件"
  node scripts/build-frontend.mjs
fi

exec node scripts/rebuild-webhook.mjs
