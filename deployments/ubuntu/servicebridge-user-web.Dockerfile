FROM nginx:1.27-alpine

WORKDIR /usr/share/nginx/html
COPY servicebridge/apps/user-web/ .
COPY deployments/ubuntu/user-web-nginx.conf /etc/nginx/conf.d/default.conf
