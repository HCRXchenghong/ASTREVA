#!/usr/bin/env bash
# 监视翻译状态变化，写入 .local-logs/translation-watch.log
LOG="/home/seron-cheng/桌面/ASTREVA-main/.local-logs/translation-watch.log"
: > "$LOG"
prev=""
for i in $(seq 1 60); do
  line=$(docker exec astreva-cms-postgres psql -U astreva_cms -d astreva_cms -At -c "SELECT string_agg(locale || ':' || status, ' | ' ORDER BY locale) FROM site_content_translations;" 2>/dev/null)
  ts=$(date '+%H:%M:%S')
  if [ "$line" != "$prev" ]; then
    echo "$ts $line" >> "$LOG"
    prev="$line"
  fi
  case "$line" in
    *running*|*pending*) sleep 30 ;;
    *) echo "$ts DONE: $line" >> "$LOG"; exit 0 ;;
  esac
done
echo "$(date '+%H:%M:%S') WATCH_TIMEOUT last=$prev" >> "$LOG"
