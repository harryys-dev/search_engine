.PHONY: build run clean dev test fmt vet

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
