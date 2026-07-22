# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.23
FROM golang:${GO_VERSION}-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Cache go modules
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy only files required by the build after the dependency layer.
COPY *.go ./
COPY internal ./internal
COPY migrations ./migrations
COPY static ./static
COPY templates ./templates

ARG VERSION="dev"
ARG COMMIT="none"
ARG BUILD_DATE="unknown"

# Build with CGO enabled (mattn/go-sqlite3 requirement)
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -buildvcs=false -mod=readonly \
    -ldflags="-w -s -X portfolio/internal/buildinfo.Version=${VERSION} -X portfolio/internal/buildinfo.Commit=${COMMIT} -X portfolio/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    -o portfolio .

# Final lightweight image
FROM debian:bookworm-slim

ARG VERSION="dev"
ARG COMMIT="none"
ARG BUILD_DATE="unknown"

# OCI Labels
LABEL org.opencontainers.image.title="CM Portfolio"
LABEL org.opencontainers.image.source="https://github.com/C4risrocks/ola-k-ase"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.revision="${COMMIT}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget && rm -rf /var/lib/apt/lists/*

# Create non-root user and prep /data volume
RUN useradd --uid 10001 --create-home --no-log-init appuser && \
    mkdir -p /data && \
    chown -R appuser:appuser /data

WORKDIR /app

COPY --from=builder --chown=appuser:appuser /app/portfolio /app/portfolio

USER appuser

ENV PORT=8080
ENV DATABASE_PATH=/data/site.db
ENV APP_ENV=production
ENV LOG_LEVEL=info

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider "http://127.0.0.1:${PORT}/health" || exit 1

ENTRYPOINT ["/app/portfolio"]
