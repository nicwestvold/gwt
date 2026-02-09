VERSION := $(shell git describe --tags --always --dirty)

.PHONY: build clean help install test

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build    Build the gwt binary"
	@echo "  clean    Remove the gwt binary"
	@echo "  help     Show this help message"
	@echo "  install  Install gwt via go install"
	@echo "  test     Run tests"

build:
	go build -ldflags "-X main.version=$(VERSION)" -o gwt .

clean:
	rm -f gwt

install:
	go install -ldflags "-X main.version=$(VERSION)" .

test:
	go test ./...
