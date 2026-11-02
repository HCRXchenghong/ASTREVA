# 阶段一：安装全部依赖（含构建 admin-web 所需的 devDependencies）。
FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
COPY frontend/package.json ./frontend/package.json
COPY admin-web/package.json ./admin-web/package.json
# postinstall 会执行 scripts/ensure-rolldown-binding.mjs，必须在 npm ci 前拷入。
COPY scripts/ensure-rolldown-binding.mjs ./scripts/ensure-rolldown-binding.mjs
RUN npm ci --no-audit --no-fund

# 阶段二：构建 Arco Design Pro Vue 控制台。
FROM deps AS build-web
COPY admin-web ./admin-web
# 控制台需要官网的静态品牌资源（logo 等），一并拷入构建上下文。
COPY frontend/public ./frontend/public
RUN npm --workspace admin-web run build

# 阶段三：运行时只保留生产依赖与构建产物。
FROM node:24-alpine
WORKDIR /app
COPY package.json package-lock.json* ./
COPY frontend/package.json ./frontend/package.json
COPY admin-web/package.json ./admin-web/package.json
COPY scripts/ensure-rolldown-binding.mjs ./scripts/ensure-rolldown-binding.mjs
RUN npm ci --omit=dev --no-audit --no-fund

COPY admin-server ./admin-server
COPY frontend ./frontend
COPY --from=build-web /app/admin-web/dist ./admin-web/dist
RUN adduser -D -H -u 1001 appuser \
  && mkdir -p /app/data /app/uploads \
  && chown -R appuser:appuser /app
USER appuser

ENV APP_ENV=production
ENV SITE_CONTENT_SEED_FILE=/app/admin-server/seed/site-content.json
EXPOSE 1337
CMD ["node", "admin-server/server.mjs"]
