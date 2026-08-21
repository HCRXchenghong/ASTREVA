FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
COPY frontend/package.json frontend/package.json
RUN npm install --workspace frontend
COPY frontend frontend
ARG PUBLIC_SITE_URL=https://www.example.com
ARG PUBLIC_SERVICEBRIDGE_URL=https://service.example.com
ARG PUBLIC_ADMIN_URL=
ENV PUBLIC_SITE_URL=$PUBLIC_SITE_URL
ENV PUBLIC_SERVICEBRIDGE_URL=$PUBLIC_SERVICEBRIDGE_URL
ENV PUBLIC_ADMIN_URL=$PUBLIC_ADMIN_URL
RUN npm --workspace frontend run build

FROM nginx:1.27-alpine
COPY --from=build /app/frontend/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
