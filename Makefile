.DEFAULT_GOAL := help

COVERPKG := ./client/...,./configuration/...,./datastore/...,./messaging/...,./observability/...,./parser/...,./security/...

.PHONY: help deps tidy tidy-check fmt fmt-check vet lint test test-race ci mockery gotest gotestcoverage gotestcoveragereport

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

deps: ## Download module dependencies
	go mod download

tidy: ## Tidy go.mod/go.sum
	go mod tidy

tidy-check: tidy ## Verify go.mod/go.sum are tidy (mirrors CI; fails if tidy produced a diff)
	git diff --exit-code -- go.mod go.sum

fmt: ## Format all Go files in place
	gofmt -w .

fmt-check: ## Check formatting without modifying files (mirrors CI)
	@diff=$$(gofmt -l .); \
	if [ -n "$$diff" ]; then \
		echo "The following files are not gofmt'ed:"; \
		echo "$$diff"; \
		exit 1; \
	fi

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (requires golangci-lint on PATH)
	golangci-lint run

test: ## Run tests
	go test ./... -count=1

test-race: ## Run tests with the race detector (mirrors CI)
	go test ./... -race -count=1

ci: deps tidy-check fmt-check vet lint test-race ## Run the full CI check locally, in the same order as .github/workflows/ci.yml

mockery: ## Regenerate interface mocks declared in .mockery.yaml (test/mock/)
	mockery

gotest: ## Run all tests with coverage instrumented across every production package (test/unit + co-located)
	go test ./... -covermode=set -coverpkg=$(COVERPKG) -coverprofile=cover.out -count=1

gotestcoverage: gotest ## Run tests and enforce the per-file coverage gate (.testcoverage.yml)
	go-test-coverage --config=./.testcoverage.yml

gotestcoveragereport: gotest ## Run tests and open an HTML coverage report (cover.html)
	go tool cover -html cover.out -o cover.html
