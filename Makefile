.PHONY: build check lint test run

build:
	go build -o haul ./cmd/haul

run: build
	./haul

lint:
	golangci-lint run ./...

test:
	go test ./...

check: lint test
	@echo "✓ all checks passed"
