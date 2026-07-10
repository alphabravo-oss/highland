# Build SPA then serve with nginx (proxies /auth /api to highland-api Service)
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# Unprivileged nginx: runs as uid 101 and listens on 8080 (no root needed to bind)
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
# HIGHLAND_API_UPSTREAM set at runtime by chart (e.g. highland-api:8080)
ENV HIGHLAND_API_UPSTREAM=highland-api:8080
ENV NGINX_ENVSUBST_FILTER=HIGHLAND_API_UPSTREAM
EXPOSE 8080
