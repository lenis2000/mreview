.PHONY: build install test lint fmt clean docker-image docker-run pnas-fixture

BIN := .bin/mreview
INSTALL_DIR ?= $(HOME)/bin
IMAGE ?= ralphex-mreview
RALPHEX_DK ?= $(HOME)/__code/ralphex/scripts/ralphex-dk.sh
PLAN := docs/plans/2026-04-22-mreview-mvp.md

build:
	@mkdir -p .bin
	go build -tags=pdfverify -o $(BIN) ./cmd/mreview

install: build
	@mkdir -p $(INSTALL_DIR)
	install -m 0755 $(BIN) $(INSTALL_DIR)/mreview
	@echo "installed $(INSTALL_DIR)/mreview"

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

# Regenerate PNAS golden fixture files.
# Step 1: re-run the format pipeline to produce main_pnas.expected.tex.
# Step 2: build the expected PDF and extract pdftotext output.
# Requires: latexmk, pdftotext on $PATH.
pnas-fixture:
	go run testdata/pnas-fixture/gen_expected.go testdata/pnas-fixture/
	@if command -v latexmk >/dev/null 2>&1 && command -v pdftotext >/dev/null 2>&1; then \
		tmpdir=$$(mktemp -d); \
		cp testdata/pnas-fixture/main_pnas.expected.tex "$$tmpdir/main_pnas.tex"; \
		cp testdata/pnas-fixture/latexmkrc "$$tmpdir/"; \
		(cd "$$tmpdir" && latexmk -pdf -interaction=nonstopmode main_pnas.tex >/dev/null 2>&1); \
		pdftotext "$$tmpdir/main_pnas.pdf" - > testdata/pnas-fixture/main_pnas.expected.txt; \
		rm -rf "$$tmpdir"; \
		echo "regenerated: main_pnas.expected.txt"; \
	else \
		echo "warning: latexmk/pdftotext not found; skipping expected.txt generation"; \
	fi
