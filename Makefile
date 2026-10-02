GO ?= go
VERSION ?= dev
PREFIX ?= $(HOME)/.local

.PHONY: build install test check dist

build:
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/wf ./cmd/wf

install: build
	install -d "$(PREFIX)/bin"
	install -m 755 bin/wf "$(PREFIX)/bin/wf"

test:
	$(GO) test -race ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

dist:
	GO="$(GO)" ./scripts/dist.sh "$(VERSION)"
