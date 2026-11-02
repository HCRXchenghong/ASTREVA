#!/bin/sh
# 给访客聊天页会随部署变化的静态资源加内容哈希文件名。
# CDN（Cloudflare）对静态资源做一年期 immutable 缓存，换文件名可让
# 每次发布立即生效，无需刷新 CDN 缓存。
set -eu
cd /usr/share/nginx/html
for f in assets/i18n.js assets/tailwind.css assets/chat.js assets/chat-boot.js assets/chat.css; do
  h=$(md5sum "$f" | cut -c1-10)
  nf=$(echo "$f" | sed "s/\.\([a-z]*\)$/.$h.\1/")
  mv "$f" "$nf"
  sed -i "s#$f#$nf#g" index.html
  echo "hashed asset: $nf"
done
