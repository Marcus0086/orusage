BIN     ?= orusage
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build install run test race vet fmt check snapshot clean

build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) .

install:
	go build -ldflags '$(LDFLAGS)' -o $(PREFIX)/bin/$(BIN) .

run: build
	./$(BIN)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: fmt vet race

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f $(BIN)
	rm -rf dist
