.PHONY: build test install release clean

GOCACHE ?= /tmp/server-watch-go-cache
VERSION ?= 0.1.0
PREFIX ?= $(HOME)/.local
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o bin/server-watch ./cmd/server-watch

install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 bin/server-watch "$(DESTDIR)$(PREFIX)/bin/server-watch"

release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o dist/server-watch-linux-amd64 ./cmd/server-watch
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o dist/server-watch-linux-arm64 ./cmd/server-watch
	cd dist && sha256sum server-watch-linux-* > SHA256SUMS

test:
	GOCACHE=$(GOCACHE) go test ./...
	npm test

clean:
	rm -f bin/server-watch dist/server-watch-linux-amd64 dist/server-watch-linux-arm64 dist/SHA256SUMS
