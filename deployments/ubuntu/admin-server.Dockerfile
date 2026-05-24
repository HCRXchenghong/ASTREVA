FROM node:24-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --omit=dev --no-audit --no-fund

COPY admin-server ./admin-server
COPY frontend ./frontend

FROM node:24-alpine
WORKDIR /app
COPY --from=build /app /app
RUN adduser -D -H -u 1001 appuser \
  && chown -R appuser:appuser /app
USER appuser

EXPOSE 1337
CMD ["node", "admin-server/server.mjs"]
