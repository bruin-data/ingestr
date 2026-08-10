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
LINT_MERGE_BASE ?= origin/main
GCI_VERSION ?= v0.14.0
GOFUMPT_VERSION ?= v0.11.0
# Pinned, not @latest. v2.12.x made its cache checkout-independent, but cached
# diagnostics still contain absolute paths from the checkout that produced
# them. That makes shared-cache results unsafe across worktrees. Re-test before
# bumping beyond the latest pre-change patch release.
GOLANGCI_LINT_VERSION ?= v2.11.4
# Built with the toolchain this module targets, not golangci-lint's own minimum.
# `go install` never downgrades but does pick the module's minimum when the base
# toolchain is older, which yields a binary that refuses to lint this repo:
# "the Go language version (go1.25) ... is lower than the targeted Go version".
GOLANGCI_LINT_INSTALL := GOTOOLCHAIN=go$(shell awk '/^go /{print $$2; exit}' go.mod) \
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# golangci-lint takes a single global lock at $TMPDIR/golangci-lint.lock and
# aborts after 5s if another instance holds it, so two checkouts linting at once
# make one of them fail outright. Its cache is Go's own DiskCache, which is
# built for concurrent multi-process use, so opting out of the lock is safe.
LINT_PARALLEL_FLAGS ?= --allow-parallel-runners
LINT_CONCURRENCY ?= 4
LINT_TIMEOUT ?= 10m
LINT_INTEGRATION_FLAGS ?= --build-tags=integration
TEST_CONCURRENCY ?= 4
REGISTRY_OUTPUT := internal/registry/imports/imports.gen.go
REGISTRY_INPUTS := cmd/genregistry/main.go pkg/source pkg/destination $(wildcard pkg/source/* pkg/destination/*)
export INGESTR_DISABLE_TELEMETRY := true
export DISABLE_TELEMETRY := true
TELEMETRY_ENV := INGESTR_DISABLE_TELEMETRY=true DISABLE_TELEMETRY=true
TEST_ENV := $(TELEMETRY_ENV) SF_DISABLE_MINICORE=true INGESTR_QUIET_PROGRESS=1

NO_COLOR=\033[0m
OK_COLOR=\033[32;01m
ERROR_COLOR=\033[31;01m

.PHONY: all clean test test-full test-python build deps generate licenses licenses-check licenses-audit licenses-audit-update licenses-notices-check lint lint-fast lint-full format lint-ci format-ci test-ci setup test-db2-integration cdc-stress-test cdc-postgres-stress-test cdc-mysql-stress-test cdc-mssql-stress-test

all: clean deps test build

deps:
	@printf "$(OK_COLOR)==> Installing dependencies$(NO_COLOR)\n"
	@go mod tidy

setup:
	@printf "$(OK_COLOR)==> Installing development tools$(NO_COLOR)\n"
	@current_version="$$(gci --version 2>/dev/null | awk '{ print "v"$$NF; exit }')"; \
	if [ "$$current_version" != "$(GCI_VERSION)" ]; then go install github.com/daixiang0/gci@$(GCI_VERSION); fi
	@current_version="$$(gofumpt -version 2>/dev/null | awk '{ print $$1; exit }')"; \
	if [ "$$current_version" != "$(GOFUMPT_VERSION)" ]; then go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION); fi
	@current_version="$$(golangci-lint version 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($$i == "version") { print "v"$$(i+1); exit } }')"; \
	if [ "$$current_version" != "$(GOLANGCI_LINT_VERSION)" ]; then $(GOLANGCI_LINT_INSTALL); fi

tools-update:
	@printf "$(OK_COLOR)==> Installing development tools$(NO_COLOR)\n"
	go install github.com/daixiang0/gci@$(GCI_VERSION)
	go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	$(GOLANGCI_LINT_INSTALL)

generate: $(REGISTRY_OUTPUT)

$(REGISTRY_OUTPUT): $(REGISTRY_INPUTS)
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
	@echo "$(OK_COLOR)==> Running unit tests (fast)$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TEST_ENV) go test -short -p "$(TEST_CONCURRENCY)" -timeout 5m ./...

test-full: generate
	@echo "$(OK_COLOR)==> Running unit tests (full)$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TEST_ENV) go test -short -race -cover -p "$(TEST_CONCURRENCY)" -timeout 5m ./...
	@$(MAKE) test-python

test-ci: test-full

test-python:
	@echo "$(OK_COLOR)==> Running Python SDK tests$(NO_COLOR)"
	@if command -v uv >/dev/null 2>&1; then \
		$(TEST_ENV) uv run --extra sdk python tests/python/test_ingestr_package.py; \
	else \
		echo "uv not found; install uv to run Python SDK tests"; \
		exit 1; \
	fi

test-integration: generate
	@echo "$(OK_COLOR)==> Running integration tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TEST_ENV) go test -tags integration -v -p 64 -parallel 8 -timeout 20m ./tests/integration/...

# Subset of integration tests that need no Docker/containers (file-based:
# sqlite/duckdb/csv/parquet/local-fs iceberg). Runs anywhere, incl. macOS CI.
test-integration-nodocker: generate
	@echo "$(OK_COLOR)==> Running Docker-free integration tests$(NO_COLOR)"
	@INTEGRATION_BACKENDS=none go test -tags integration -v -p 64 -parallel 8 -timeout 15m \
		-run '^(TestDestinations_|TestColumn|TestStaging_|TestIceberg|TestCustomQuery_|TestDeleteInsert|TestSnakeCase|TestDirectNaming|TestInvalidSchemaNaming|TestDuckDB|TestProgressJSON|TestChessSource|TestAppLovin|TestPostgresCDC_URISchemes)' \
		-skip '^TestIcebergCatalogBackends$$' ./tests/integration/...

# The CDC stress tests share one harness: a queue-based load generator that
# tracks the target ops/sec (STRESS_OPS_PER_SEC, default 1000) as closely as
# the source engine allows, parallel ingestion into PostgreSQL and DuckDB
# destinations at the same time, and post-run verification that both
# destinations hold an exact replica of the source (aggregates plus row-by-row
# canonical comparison). Tune with STRESS_OPS_PER_SEC, STRESS_LOAD_DURATION,
# STRESS_WORKERS, STRESS_SEED_ROWS:
#   STRESS_OPS_PER_SEC=5000 STRESS_LOAD_DURATION=5m make cdc-postgres-stress-test

# Resolve DOCKER_HOST from the active docker context so testcontainers finds
# non-default daemons (OrbStack, Colima) in every stress target.
define STRESS_ENV_SETUP
if [ -f test.env ]; then . ./test.env; fi; \
resolved_docker_host="$${DOCKER_HOST:-$$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null)}"; \
if [ -n "$$resolved_docker_host" ]; then export DOCKER_HOST="$$resolved_docker_host"; fi
endef

# Run every CDC stress test back to back (~20 minutes with default profiles).
cdc-stress-test: cdc-postgres-stress-test cdc-mysql-stress-test cdc-mssql-stress-test

# High-volume PostgreSQL CDC accuracy and schema-churn test streaming into
# PostgreSQL and DuckDB in parallel (~6 minutes with the default profile),
# followed by focused schema-evolution regressions. The high-volume workload
# remains gated behind the `stress` build tag.
cdc-postgres-stress-test: generate
	@echo "$(OK_COLOR)==> Running PostgreSQL CDC complex-workload stress test (default profile: ~6m)$(NO_COLOR)"
	@$(STRESS_ENV_SETUP); \
	$(TEST_ENV) go test -tags stress -count=1 -v -timeout 30m -run '^TestPostgresCDC_StressComplexWorkload$$' ./tests/integration/
	@echo "$(OK_COLOR)==> Running PostgreSQL CDC schema-evolution regressions for DuckDB and MySQL$(NO_COLOR)"
	@$(STRESS_ENV_SETUP); \
	INTEGRATION_BACKENDS=postgres,mysql $(TEST_ENV) go test -tags integration -count=1 -v -timeout 5m -run '^TestPostgresCDC_StreamingSchemaEvolution_(DuckDB|MySQL)$$' ./tests/integration/

# High-volume MySQL CDC accuracy and performance test running parallel batch
# ingestion processes into PostgreSQL and DuckDB (~6 minutes with the default
# profile), plus focused correctness regressions for protocol modes and failure
# recovery. Gated behind the `stress` build tag.
cdc-mysql-stress-test: generate
	@echo "$(OK_COLOR)==> Running MySQL CDC stress and correctness regression tests (default profile: ~6m)$(NO_COLOR)"
	@$(STRESS_ENV_SETUP); \
	$(TEST_ENV) go test -tags stress -count=1 -v -timeout 30m -run '^TestMySQLCDC_Stress' ./pkg/source/mysql ./tests/integration/

# High-volume SQL Server CDC accuracy and schema-churn test (~7 minutes with
# the default profile). Streams multi-table CDC into PostgreSQL and DuckDB in
# parallel under load with late tables, capture-instance recreation for
# add/rename/widen DDL, a transactional delete-all wipe, PK moves, deletes,
# and wide type coverage, then verifies exact row-by-row parity in both
# destinations. Gated behind the `stress` build tag.
cdc-mssql-stress-test: generate
	@echo "$(OK_COLOR)==> Running SQL Server CDC complex-workload stress test (default profile: ~7m)$(NO_COLOR)"
	@$(STRESS_ENV_SETUP); \
	$(TEST_ENV) go test -tags stress -count=1 -v -timeout 30m -run '^TestMSSQLCDC_StressComplexWorkload$$' ./tests/integration/

test-db2-integration: generate
	@echo "$(OK_COLOR)==> Running Db2 integration tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && INGESTR_TEST_DB2=1 $(TEST_ENV) go test -tags integration -count=1 -v -timeout 10m ./pkg/source/db2 -run TestDb2SourceWithIBMContainer

test-conformance:
	@echo "$(OK_COLOR)==> Running destination standards tests$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TEST_ENV) go test -tags integration -v -timeout 10m ./tests/integration -run TestDestinations_

comma := ,
# Run destination conformance for only the given backend(s), skipping the Docker
# setup for every other backend. Backends with no container (snowflake, bigquery)
# need no Docker at all. Comma-separate for multiple. Examples:
#   make test-conformance-only BACKENDS=snowflake
#   make test-conformance-only BACKENDS=snowflake,postgres
test-conformance-only:
	@if [ -z "$(BACKENDS)" ]; then echo "$(ERROR_COLOR)==> BACKENDS is required, e.g. make test-conformance-only BACKENDS=snowflake$(NO_COLOR)"; exit 1; fi
	@echo "$(OK_COLOR)==> Running destination standards tests for: $(BACKENDS)$(NO_COLOR)"
	@if [ -f test.env ]; then . ./test.env; fi && $(TEST_ENV) \
		INTEGRATION_BACKENDS=$(BACKENDS) go test -tags integration -v -timeout 15m ./tests/integration \
		-run 'TestDestinations_.*/($(subst $(comma),|,$(BACKENDS)))'


# Format code and run the fast changed-package lint for local cleanup.
format:
	@echo "$(OK_COLOR)==> Formatting code$(NO_COLOR)"
	@gci write cmd pkg internal tests main.go
	@gofumpt -w cmd pkg internal tests main.go
	@$(MAKE) lint

# Fast edit-loop check on changed Go packages. `go vet` is deliberately absent
# because govet is already enabled here.
lint: generate
	@echo "$(OK_COLOR)==> Running fast linters on packages changed since $(LINT_MERGE_BASE)$(NO_COLOR)"
	@LINT_MERGE_BASE="$(LINT_MERGE_BASE)" LINT_CONCURRENCY="$(LINT_CONCURRENCY)" LINT_TIMEOUT="$(LINT_TIMEOUT)" LINT_PARALLEL_FLAGS="$(LINT_PARALLEL_FLAGS)" LINT_ENABLE_ONLY="errcheck,govet,ineffassign" ./hack/lint-changed.sh

lint-fast: lint

# Full local check, including files gated behind the integration build tag.
lint-full: generate
	@echo "$(OK_COLOR)==> Running all linters across the repository$(NO_COLOR)"
	@golangci-lint run --timeout "$(LINT_TIMEOUT)" --concurrency "$(LINT_CONCURRENCY)" $(LINT_PARALLEL_FLAGS) $(LINT_INTEGRATION_FLAGS) ./...

# CI: Check formatting without modifying files (fails if changes needed)
format-ci: generate
	@echo "$(OK_COLOR)==> Checking code formatting$(NO_COLOR)"
	@DIFF="$$(gci list cmd pkg internal tests main.go 2>&1)$$(gofumpt -d cmd pkg internal tests main.go 2>&1)"; \
	if [ -n "$$DIFF" ]; then \
		echo "$(ERROR_COLOR)Files need formatting:$(NO_COLOR)"; \
		echo "$$DIFF"; \
		echo "$(ERROR_COLOR)Run 'make format' locally and commit.$(NO_COLOR)"; \
		exit 1; \
	fi
	@echo "$(OK_COLOR)All files are properly formatted$(NO_COLOR)"

# CI: Full formatting and lint checks.
lint-ci: format-ci lint-full
	@echo "$(OK_COLOR)All checks passed$(NO_COLOR)"
