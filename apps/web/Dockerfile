# Build SPA then serve with nginx (proxies /auth /api to highland-api Service)
#
# Base images: pin by digest when a reviewed digest is available (ADR-0006).
# Capture with: docker buildx imagetools inspect <image>:<tag>
# Prefer: FROM image:tag@sha256:<digest>

# node:26-alpine
# TODO: pin by digest once reviewed
#   FROM node:26-alpine@sha256:<digest> AS build
FROM node:26-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN apk add --no-cache brotli \
    && find /app/dist -type f \( -name '*.css' -o -name '*.html' -o -name '*.js' -o -name '*.json' -o -name '*.svg' -o -name '*.txt' -o -name '*.xml' \) -size +1024c -exec gzip -9 -k {} \; \
    && find /app/dist -type f \( -name '*.css' -o -name '*.html' -o -name '*.js' -o -name '*.json' -o -name '*.svg' -o -name '*.txt' -o -name '*.xml' \) -size +1024c -exec brotli --quality=11 --keep {} \;

# nginxinc/nginx-unprivileged:1.31-alpine
# TODO: pin by digest once reviewed
#   FROM nginxinc/nginx-unprivileged:1.31-alpine@sha256:<digest>
# Unprivileged nginx: runs as uid 101 and listens on 8080 (no root needed to bind)
FROM nginxinc/nginx-unprivileged:1.31-alpine
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
# HIGHLAND_API_UPSTREAM set at runtime by chart (e.g. highland-api:8080)
ENV HIGHLAND_API_UPSTREAM=highland-api:8080
ENV HIGHLAND_CEPH_DASHBOARD_UPSTREAM=127.0.0.1:9
ENV HIGHLAND_CEPH_DASHBOARD_TLS_NAME=localhost
ENV HIGHLAND_CEPH_DASHBOARD_CA_FILE=/etc/ssl/certs/ca-certificates.crt
ENV NGINX_ENVSUBST_FILTER=HIGHLAND_
EXPOSE 8080
