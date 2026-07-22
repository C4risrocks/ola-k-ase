.PHONY: run build test lint docker clean

VERSION ?= $(shell git describe --tags --always --dirty || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -w -s -X portfolio/internal/buildinfo.Version=$(VERSION) -X portfolio/internal/buildinfo.Commit=$(COMMIT) -X portfolio/internal/buildinfo.BuildDate=$(BUILD_DATE)

run:
	@echo "Starting development server..."
	DATABASE_PATH=./data/site.db APP_ENV=development LOG_LEVEL=debug go run -ldflags="$(LDFLAGS)" .

build:
	@echo "Building binary..."
	mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/portfolio .

test:
	@echo "Running tests..."
	go test -v -race ./...

lint:
	@echo "Running golangci-lint..."
	golangci-lint run ./...

docker:
	@echo "Building Docker image..."
	docker buildx build --load \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t portfolio:latest .

clean:
	@echo "Cleaning up..."
	rm -rf bin/
