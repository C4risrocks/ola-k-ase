# syntax=docker/dockerfile:1.4
FROM golang:1.23-bookworm AS builder

RUN apt-get update && apt-get install -y gcc g++ make libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Cache go modules
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy everything else
COPY . .

ARG VERSION="dev"
ARG COMMIT="none"
ARG BUILD_DATE="unknown"

# Build with CGO enabled (mattn/go-sqlite3 requirement)
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o portfolio .

# Final lightweight image
FROM debian:bookworm-slim

# OCI Labels
LABEL org.opencontainers.image.title="CM Portfolio"
LABEL org.opencontainers.image.source="https://github.com/C4risrocks/ola-k-ase"

RUN apt-get update && apt-get install -y ca-certificates wget && rm -rf /var/lib/apt/lists/*

# Create non-root user and prep /data volume
RUN useradd -u 10001 appuser && \
    mkdir -p /data && \
    chown -R appuser:appuser /data

WORKDIR /app

COPY --from=builder /app/portfolio /app/portfolio

# Ensure the binary is executable and owned by appuser
RUN chown appuser:appuser /app/portfolio && chmod +x /app/portfolio

USER appuser

ENV PORT=8080
ENV DATABASE_PATH=/data/site.db
ENV APP_ENV=production
ENV LOG_LEVEL=info

EXPOSE ${PORT}

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:${PORT}/health || exit 1

ENTRYPOINT ["/app/portfolio"]
