.PHONY: build test lint fmt clean

BIN := .bin/mreview

build:
	@mkdir -p .bin
	go build -o $(BIN) ./cmd/mreview

test:
	go test -cover ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...
	goimports -w .

clean:
	rm -rf .bin coverage.out coverage.html
