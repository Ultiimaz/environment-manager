# Build frontend
FROM node:20-alpine AS frontend-builder
RUN corepack enable
WORKDIR /app/frontend
COPY frontend/package.json ./
RUN pnpm install
COPY frontend/ ./
RUN pnpm build

# Build backend
FROM golang:1.24-alpine AS backend-builder
WORKDIR /app
RUN apk add --no-cache git
COPY backend/go.mod ./
RUN go mod download || true
COPY backend/ ./
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

# Final image
FROM alpine:3.19

# Install git and git-lfs for backup operations
RUN apk add --no-cache git git-lfs ca-certificates tzdata

WORKDIR /app

# Copy built artifacts
COPY --from=backend-builder /server /app/server
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
