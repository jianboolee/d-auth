# syntax=docker/dockerfile:1

# ============================================================
# d-auth 发布镜像（全栈单镜像）
#
#   前端：Vite 构建 web/dist，运行时由 nginx 托管
#   后端：Go 编译 server / migrate
#   运行时：nginx 监听 80（对外经 APP_PORT=8680 映射），
#           /v1、/health、/static 反代到 127.0.0.1:8681
# ============================================================

FROM node:22-alpine AS web

WORKDIR /src
COPY package.json package-lock.json ./
COPY packages ./packages
COPY web ./web
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
RUN npm run build -w @donktech/d-auth-sdk
WORKDIR /src/web
RUN npm run build

FROM golang:1.25-alpine AS api

WORKDIR /src
COPY api/go.mod api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY api/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.21

RUN apk add --no-cache nginx ca-certificates tzdata wget

WORKDIR /app

COPY --from=api /out/server /app/server
COPY --from=api /out/migrate /app/migrate
COPY api/migrations /app/migrations
COPY --from=web /src/web/dist /usr/share/nginx/html
COPY docker/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && mkdir -p /app/data

# HTTP_PORT 是容器内 API 端口，只给 nginx 回环访问；对外是宿主机 8680 -> 容器 80
ENV APP_ENV=production \
    HTTP_PORT=8681 \
    DAUTH_JWT_PRIVATE_KEY_PATH=/app/data/jwt-private.pem \
    RUN_MIGRATIONS=true

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8681/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["serve"]
