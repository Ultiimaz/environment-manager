# Build frontend
FROM node:20-alpine AS frontend-builder
RUN corepack enable
WORKDIR /app/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# Build backend
FROM golang:1.24-alpine AS backend-builder
WORKDIR /app
RUN apk add --no-cache git
COPY backend/go.mod ./
RUN go mod download || true
COPY backend/ ./
ARG VERSION=dev
# -s -w strips the symbol table + DWARF, ~25% smaller binary.
# -X main.version stamps the build with the Git SHA / release tag.
# -trimpath drops the local build path, keeps the binary reproducible.
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /envm ./cmd/envm

# Final image. Alpine 3.21 ships docker-cli 27.x which speaks Docker API
# 1.47+; 3.19's docker-cli 24.x reports API 1.43 and is rejected by the
# host daemon (Docker 29.x, MinAPI 1.44) when builder.Runner shells out.
FROM alpine:3.21

# Install git, git-lfs, and the docker CLI (with compose v2 plugin) so the
# compose handler can shell out to `docker-compose` when rebuilding linked
# projects. DOCKER_HOST points at the mounted socket.
RUN apk add --no-cache git git-lfs ca-certificates tzdata docker-cli docker-cli-compose
# Provide the legacy `docker-compose` command name used by the code paths.
RUN printf '#!/bin/sh\nexec docker compose "$@"\n' > /usr/local/bin/docker-compose \
 && chmod +x /usr/local/bin/docker-compose

WORKDIR /app

# Copy built artifacts
COPY --from=backend-builder /server /app/server
COPY --from=backend-builder /envm /usr/local/bin/envm
COPY --from=frontend-builder /app/frontend/dist /app/static

# Create data directory
RUN mkdir -p /app/data

# Environment variables
ENV GIN_MODE=release
ENV DATA_DIR=/app/data
ENV STATIC_DIR=/app/static
ENV PORT=8080

EXPOSE 8080

ENTRYPOINT ["/app/server"]
