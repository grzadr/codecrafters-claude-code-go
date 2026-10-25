.PHONY: all fmt test coverage coverage-html lint clean

COVERAGE_FILE ?= coverage.out

all: fmt test coverage

## fmt: Format all Go source files
fmt:
	@echo "==> Formatting code..."
	go fmt ./...

## lint: Run linter
lint:
	@echo "==> Running linter..."
	golangci-lint run

## test: Run tests with race detection
test:
	@echo "==> Running tests..."
	go test -v -race ./...

## coverage: Run tests and show statement coverage report
coverage:
	@echo "==> Running tests with coverage..."
	go test -coverprofile=$(COVERAGE_FILE) ./...
	@echo ""
	@echo "==> Coverage summary:"
	go tool cover -func=$(COVERAGE_FILE)

## coverage-html: Generate HTML coverage report
coverage-html: coverage
	@echo "==> Generating HTML coverage report..."
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "Report saved to coverage.html"

## clean: Remove build and coverage artifacts
clean:
	@echo "==> Cleaning artifacts..."
	rm -f $(COVERAGE_FILE) coverage.html main

