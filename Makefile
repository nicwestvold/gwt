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
	go build -o gwt .

clean:
	rm -f gwt

install:
	go install .

test:
	go test ./...
