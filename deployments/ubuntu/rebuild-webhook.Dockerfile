FROM node:24-alpine

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --omit=dev --no-audit --no-fund

COPY scripts ./scripts
COPY frontend ./frontend
COPY admin-server ./admin-server

CMD ["node", "scripts/rebuild-webhook.mjs"]
