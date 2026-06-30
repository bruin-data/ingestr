NAME=ingestr$(shell if [ "$(shell go env GOOS)" = "windows" ]; then echo .exe; fi)
BUILD_DIR ?= bin
BUILD_SRC=.
VERSION ?= dev
GO_LICENSES_MODULE ?= github.com/google/go-licenses@v1.6.0
LICENSE_DISALLOWED_TYPES ?= forbidden,restricted,unknown
LICENSE_TARGETS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
LICENSE_CHECK_TARGETS ?= linux/amd64
LICENSE_INCLUDE_TESTS ?= true
LICENSE_CHECK_INCLUDE_TESTS ?= false
LICENSE_AUDIT_TARGETS ?= $(LICENSE_CHECK_TARGETS)
LICENSE_AUDIT_INCLUDE_TESTS ?= $(LICENSE_CHECK_INCLUDE_TESTS)
LICENSE_AUDIT_NEW_STATUS ?= needs-review
export INGESTR_DISABLE_TELEMETRY := true
export DISABLE_TELEMETRY := true
TELEMETRY_ENV := INGESTR_DISABLE_TELEMETRY=true DISABLE_TELEMETRY=true

NO_COLOR=\033[0m
OK_COLOR=\033[32;01m
ERROR_COLOR=\033[31;01m

.PHONY: all clean test test-python build deps generate licenses licenses-check licenses-audit licenses-audit-update licenses-notices-check lint format lint-ci format-ci test-ci setup test-db2-integration

all: clean deps test build

deps:
	@printf "$(OK_COLOR)==> Installing dependencies$(NO_COLOR)\n"
	@go mod tidy

setup:
	@printf "$(OK_COLOR)==> Installing development tools$(NO_COLOR)\n"
	@command -v gci >/dev/null 2>&1 || go install github.com/daixiang0/gci@latest
	@command -v gofumpt >/dev/null 2>&1 || go install mvdan.cc/gofumpt@latest
	@command -v golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

tools-update:
	@printf "$(OK_COLOR)==> Installing development tools$(NO_COLOR)\n"
	go install github.com/daixiang0/gci@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	

generate:
	@echo "$(OK_COLOR)==> Generating registry imports$(NO_COLOR)"
	@go run ./cmd/genregistry

licenses: generate
	@echo "$(OK_COLOR)==> Updating third-party license notices$(NO_COLOR)"
	@GO_LICENSES_MODULE="$(GO_LICENSES_MODULE)" LICENSE_DISALLOWED_TYPES="$(LICENSE_DISALLOWED_TYPES)" LICENSE_TARGETS="$(LICENSE_TARGETS)" LICENSE_INCLUDE_TESTS="$(LICENSE_INCLUDE_TESTS)" ./hack/update-third-party-licenses.sh

licenses-check: generate
	@echo "$(OK_COLOR)==> Checking third-party license policy$(NO_COLOR)"
	@GO_LICENSES_MODULE="$(GO_LICENSES_MODULE)" LICENSE_DISALLOWED_TYPES="$(LICENSE_DISALLOWED_TYPES)" LICENSE_TARGETS="$(LICENSE_CHECK_TARGETS)" LICENSE_INCLUDE_TESTS="$(LICENSE_CHECK_INCLUDE_TESTS)" ./hack/update-third-party-licenses.sh --policy-only

licenses-audit: generate
	@echo "$(OK_COLOR)==> Checking third-party license audit lock$(NO_COLOR)"
	@GO_LICENSES_MODULE="$(GO_LICENSES_MODULE)" LICENSE_AUDIT_TARGETS="$(LICENSE_AUDIT_TARGETS)" LICENSE_AUDIT_INCLUDE_TESTS="$(LICENSE_AUDIT_INCLUDE_TESTS)" ./hack/license-audit.sh --check

licenses-audit-update: generate
	@echo "$(OK_COLOR)==> Updating third-party license audit lock$(NO_COLOR)"
	@GO_LICENSES_MODULE="$(GO_LICENSES_MODULE)" LICENSE_AUDIT_TARGETS="$(LICENSE_AUDIT_TARGETS)" LICENSE_AUDIT_INCLUDE_TESTS="$(LICENSE_AUDIT_INCLUDE_TESTS)" LICENSE_AUDIT_NEW_STATUS="$(LICENSE_AUDIT_NEW_STATUS)" ./hack/license-audit.sh --write

licenses-notices-check: generate
	@echo "$(OK_COLOR)==> Checking third-party license notices$(NO_COLOR)"
	@GO_LICENSES_MODULE="$(GO_LICENSES_MODULE)" LICENSE_DISALLOWED_TYPES="$(LICENSE_DISALLOWED_TYPES)" LICENSE_TARGETS="$(LICENSE_TARGETS)" LICENSE_INCLUDE_TESTS="$(LICENSE_INCLUDE_TESTS)" ./hack/update-third-party-licenses.sh --check


build: generate deps
	@echo "$(OK_COLOR)==> Building the application...$(NO_COLOR)"
	@mkdir -p $(BUILD_DIR)
	@go build -v -ldflags="-s -w -X github.com/bruin-data/ingestr/cmd.Version=$(VERSION)" -o "$(BUILD_DIR)/$(NAME)" "$(BUILD_SRC)"

clean:
	@rm -rf ./bin

run: build
	@./$(BUILD_DIR)/$(NAME) $(ARGS)


test: generate
	@echo "$(OK_COLOR)==> Running unit tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TELEMETRY_ENV) go test -short -race -cover -timeout 5m ./...
	@$(MAKE) test-python

test-python:
	@echo "$(OK_COLOR)==> Running Python SDK tests$(NO_COLOR)"
	@if command -v uv >/dev/null 2>&1; then \
		uv run --extra sdk python tests/python/test_ingestr_package.py; \
	else \
		echo "uv not found; install uv to run Python SDK tests"; \
		exit 1; \
	fi

test-integration: generate
	@echo "$(OK_COLOR)==> Running integration tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TELEMETRY_ENV) go test -tags integration -v -p 64 -parallel 64 -timeout 20m ./tests/integration/...

test-db2-integration: generate
	@echo "$(OK_COLOR)==> Running Db2 integration tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && INGESTR_TEST_DB2=1 $(TELEMETRY_ENV) go test -tags integration -count=1 -v -timeout 10m ./pkg/source/db2 -run TestDb2SourceWithIBMContainer

test-conformance:
	@echo "$(OK_COLOR)==> Running destination standards tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TELEMETRY_ENV) go test -tags integration -v -timeout 10m ./tests/integration -run TestDestinations_


# Format code and run linters (for local development)
format: generate
	@echo "$(OK_COLOR)==> Formatting code$(NO_COLOR)"
	@gci write cmd pkg internal tests main.go
	@gofumpt -w cmd pkg internal tests main.go
	@echo "$(OK_COLOR)==> Running linters$(NO_COLOR)"
	@go vet ./...
	@golangci-lint run --timeout 10m ./...
	wait

# Just run linters without formatting
lint: generate
	@echo "$(OK_COLOR)==> Running linters$(NO_COLOR)"
	@go vet ./...
	@golangci-lint run --timeout 10m ./...

# CI: Check formatting without modifying files (fails if changes needed)
format-ci: generate
	@echo "$(OK_COLOR)==> Checking code formatting$(NO_COLOR)"
	@DIFF=$$(gofumpt -d cmd pkg internal tests main.go 2>&1); \
	if [ -n "$$DIFF" ]; then \
		echo "$(ERROR_COLOR)Files need formatting:$(NO_COLOR)"; \
		echo "$$DIFF"; \
		echo "$(ERROR_COLOR)Run 'make format' locally and commit.$(NO_COLOR)"; \
		exit 1; \
	fi
	@echo "$(OK_COLOR)All files are properly formatted$(NO_COLOR)"

# CI: Full lint check (format check + linters)
lint-ci: format-ci generate
	@echo "$(OK_COLOR)==> Running linters (CI)$(NO_COLOR)"
	@go vet ./...
	@golangci-lint run --timeout 10m ./...
	@echo "$(OK_COLOR)All checks passed$(NO_COLOR)"
