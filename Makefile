.PHONY: build test install release clean

GOCACHE ?= /tmp/sss-go-cache
VERSION ?= 0.1.0
PREFIX ?= $(HOME)/.local
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o bin/sss ./cmd/sss

install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 bin/sss "$(DESTDIR)$(PREFIX)/bin/sss"

release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o dist/sss-linux-amd64 ./cmd/sss
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o dist/sss-linux-arm64 ./cmd/sss
	cd dist && sha256sum sss-linux-* > SHA256SUMS

test:
	GOCACHE=$(GOCACHE) go test ./...
	npm test

clean:
	rm -f bin/sss dist/sss-linux-amd64 dist/sss-linux-arm64 dist/SHA256SUMS
