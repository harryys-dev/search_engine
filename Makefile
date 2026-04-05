.PHONY: build run clean dev test fmt vet lint ci

build:
	@echo "Building backend..."
	@go build -o bin/server ./cmd/server
	@echo "Building frontend..."
	@cd web && npm run build
	@echo "Build complete!"

run: build
	@./bin/server

dev:
	@go run ./cmd/server/main.go

clean:
	@rm -rf bin/
	@rm -rf web/dist/
	@rm -rf uploads/*
	@echo "Cleaned!"

fmt:
	@go fmt ./...

vet:
	@go vet ./...

test:
	@go test -v ./...

lint:
	@echo "Linting backend..."
	@golangci-lint run ./...
	@echo "Linting frontend..."
	@cd web && npx eslint src/

ci: lint test build
	@echo "CI passed!"