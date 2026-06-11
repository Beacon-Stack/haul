.PHONY: build check lint test run web

build:
	go build -o bin/haul ./cmd/haul

run: build
	./bin/haul

web:
	cd web/ui && npm run build

lint:
	golangci-lint run ./...

test:
	go test ./...

check: lint test
	@echo "all checks passed"
