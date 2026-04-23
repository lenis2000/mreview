.PHONY: build test lint fmt clean docker-image docker-run

BIN := .bin/mreview
IMAGE ?= ralphex-mreview
RALPHEX_DK ?= $(HOME)/__code/ralphex/scripts/ralphex-dk.sh
PLAN := docs/plans/2026-04-22-mreview-mvp.md

build:
	@mkdir -p .bin
	go build -o $(BIN) ./cmd/mreview

test:
	go test -cover ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found; falling back to go vet"; \
		go vet ./...; \
	fi

fmt:
	go fmt ./...
	goimports -w .

clean:
	rm -rf .bin coverage.out coverage.html

docker-image:
	docker build -t $(IMAGE) .

docker-run:
	@test -x $(RALPHEX_DK) || { echo "error: docker wrapper not found at $(RALPHEX_DK)"; exit 1; }
	RALPHEX_IMAGE=$(IMAGE) $(RALPHEX_DK) $(PLAN)
