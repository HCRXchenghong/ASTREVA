FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
COPY frontend/package.json frontend/package.json
RUN npm install --workspace frontend
COPY frontend frontend
ARG PUBLIC_SITE_URL=https://www.example.com
ARG PUBLIC_STRAPI_URL=http://cms:1337
ARG PUBLIC_SERVICEBRIDGE_URL=https://service.example.com
ARG STRAPI_API_TOKEN=
ENV PUBLIC_SITE_URL=$PUBLIC_SITE_URL
ENV PUBLIC_STRAPI_URL=$PUBLIC_STRAPI_URL
ENV PUBLIC_SERVICEBRIDGE_URL=$PUBLIC_SERVICEBRIDGE_URL
ENV STRAPI_API_TOKEN=$STRAPI_API_TOKEN
RUN npm --workspace frontend run build

FROM nginx:1.27-alpine
COPY --from=build /app/frontend/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
