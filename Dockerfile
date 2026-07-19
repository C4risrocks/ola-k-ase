# Build stage
FROM golang:alpine AS builder

# Install build dependencies for cgo (SQLite needs it)
RUN apk add --no-cache build-base

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -o portfolio .

# Final stage
FROM alpine:latest

# Install dependencies (ca-certificates, tzdata)
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary and assets
COPY --from=builder /app/portfolio .
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates

# Create directory for SQLite persistent database
RUN mkdir -p /app/data

# Expose default port
EXPOSE 8080

# Defaults
ENV PORT=8080
ENV DB_PATH=/app/data/portfolio.db

CMD ["./portfolio"]
