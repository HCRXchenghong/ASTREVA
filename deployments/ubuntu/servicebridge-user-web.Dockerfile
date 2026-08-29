FROM nginx:1.27-alpine

WORKDIR /usr/share/nginx/html
COPY servicebridge/apps/user-web/ .
COPY deployments/ubuntu/user-web-nginx.conf /etc/nginx/conf.d/default.conf

# 给会随部署变化的静态资源加内容哈希文件名：CDN（Cloudflare）对静态资源做
# 一年期 immutable 缓存，换文件名可让每次发布立即生效，无需刷新 CDN 缓存。
COPY deployments/ubuntu/user-web-hash-assets.sh /tmp/user-web-hash-assets.sh
RUN sh /tmp/user-web-hash-assets.sh && rm /tmp/user-web-hash-assets.sh
