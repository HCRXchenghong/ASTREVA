FROM node:24-alpine

WORKDIR /app
RUN addgroup -g 10001 -S app \
  && adduser -S -D -H -u 10001 -G app app \
  && mkdir -p /published /home/appuser \
  && chown -R app:app /published /home/appuser

COPY package.json package-lock.json* ./
COPY frontend/package.json ./frontend/package.json
COPY admin-web/package.json ./admin-web/package.json
# postinstall 会执行 scripts/ensure-rolldown-binding.mjs，必须在 npm ci 前拷入。
COPY scripts/ensure-rolldown-binding.mjs ./scripts/ensure-rolldown-binding.mjs
RUN npm ci --no-audit --no-fund

COPY scripts ./scripts
COPY frontend ./frontend
COPY admin-server ./admin-server
RUN chown -R app:app /app \
  && chmod +x /app/scripts/rebuild-entrypoint.sh

ENV HOME=/home/appuser
USER 10001:10001
ENTRYPOINT ["/app/scripts/rebuild-entrypoint.sh"]
