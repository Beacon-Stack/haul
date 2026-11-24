.PHONY: build check deadcode deadcode/go lint test test/frontend typecheck run web

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

deadcode: deadcode/go
	cd web/ui && npm run deadcode

deadcode/go:
	@out="$$(go run golang.org/x/tools/cmd/deadcode@v0.53.0 -test ./... | grep -v node_modules)"; echo "$$out"; test -z "$$out"

typecheck:
	cd web/ui && npm run typecheck

test/frontend:
	cd web/ui && npm test

check: lint deadcode test typecheck test/frontend
	@echo "all checks passed"
