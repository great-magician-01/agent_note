# syntax=docker/dockerfile:1

# ============================================================
# 阶段 1：构建前端（Vue 3 + Vite，产物在 /build/web/dist）
# 用 glibc 基础镜像：rolldown / lightningcss 的原生绑定对 musl 支持不全
# ============================================================
FROM node:22-bookworm-slim AS web-builder

WORKDIR /build/web

# 先拷贝 lockfile，依赖未变时走构建缓存
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

# ============================================================
# 阶段 2：构建后端（静态编译，CGO 关闭）
# ============================================================
FROM golang:1.25-alpine AS go-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# web/dist 由阶段 1 提供，不依赖仓库内文件
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server .

# ============================================================
# 阶段 3：运行镜像（后端直接托管前端页面）
# ============================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

WORKDIR /app

COPY --from=go-builder /out/server /app/server
COPY --from=web-builder /build/web/dist /app/web/dist
RUN mkdir -p /app/logs /app/uploads && chown -R app:app /app

USER app

ENV SERVER_PORT=7562 \
    WEB_DIST_DIR=/app/web/dist \
    LOG_DIR=/app/logs \
    UPLOAD_DIR=/app/uploads

# 数据库 / 鉴权配置通过环境变量注入：
#   DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SCHEMA
#   JWT_SECRET ADMIN_USERNAME ADMIN_PASSWORD
EXPOSE 7562

CMD ["/app/server"]
