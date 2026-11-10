GO ?= go
BIN ?= yangjian
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/rbee3u/yangjian/internal/version.Version=$(VERSION) \
	-X github.com/rbee3u/yangjian/internal/version.Commit=$(COMMIT) \
	-X github.com/rbee3u/yangjian/internal/version.Date=$(DATE)

.PHONY: build install test test-live vet verify clean

build:
	$(GO) build -trimpath -buildvcs -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/yangjian

install:
	$(GO) install -trimpath -buildvcs -ldflags "$(LDFLAGS)" ./cmd/yangjian

test:
	$(GO) test ./...

test-live:
	YANGJIAN_LIVE=1 $(GO) test ./...

vet:
	$(GO) vet ./...

verify:
	$(GO) vet ./...
	@command -v govulncheck >/dev/null 2>&1 || { echo "govulncheck is not installed"; exit 1; }
	govulncheck ./...

clean:
	rm -rf bin
