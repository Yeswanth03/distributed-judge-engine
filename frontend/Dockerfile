# The frontend for Docker Compose: build the app, then serve the static files with nginx, which also proxies /api
# to the gateway exactly as the Vite dev server does (see nginx.conf.template).

# Node 24 (LTS) ships npm 11, the version package-lock.json was written with; npm 10 rejects it.
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# The official image renders /etc/nginx/templates/*.template with environment variables at start-up.
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
ENV GATEWAY_URL=http://gateway:8000
EXPOSE 80
HEALTHCHECK --interval=5s --timeout=3s --retries=10 CMD wget -qO- http://127.0.0.1/healthz || exit 1
