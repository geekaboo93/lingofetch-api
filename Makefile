.PHONY: help build-extension build-api clean test docker-build docker-up docker-down dev dev-down verify

# Default target
help:
	@echo "LingoFetch - Available Commands"
	@echo "================================"
	@echo ""
	@echo "🐳 Extension (Docker - Recommended):"
	@echo "  make build-extension      - Build extension with Docker (Node 18)"
	@echo "  make verify               - Verify extension build is complete"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build    - Build all Docker images"
	@echo "  make docker-up       - Start all services with Docker Compose"
	@echo "  make docker-down     - Stop all Docker services"
	@echo "  make dev             - Start all services in HOT-RELOAD mode (Docker)"
	@echo "  make dev-down        - Stop hot-reload services"
	@echo "  make docker-logs     - View Docker logs"
	@echo ""
	@echo "Utilities (Local):"
	@echo "  make run-api         - Run backend API locally (requires Go)"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make test            - Run tests"
	@echo "  make deploy          - Deploy to GCP Cloud Run"

# Extension Development (Docker)
build-extension:
	@./build-extension-docker.sh

verify:
	@echo "Verifying extension build..."
	@./verify-extension.sh

# Backend Development
run-api:
	@echo "Starting backend API..."
	go run cmd/api/main.go

test:
	@echo "Running tests..."
	go test ./...

# Docker Commands
docker-build:
	@echo "Building Docker images..."
	docker-compose --profile build build

docker-up:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d api

docker-down:
	@echo "Stopping Docker services..."
	docker-compose down

docker-logs:
	@echo "Viewing Docker logs..."
	docker-compose logs -f

dev:
	@echo "🚀 Starting LingoFetch in Hot-Reload mode (Docker)..."
	docker-compose --profile dev up --build

dev-down:
	@echo "🛑 Stopping hot-reload services..."
	docker-compose --profile dev down

docker-extension:
	@echo "Building extension with Docker..."
	docker-compose --profile build up extension-builder

# Deployment
deploy:
	@echo "Deploying to GCP Cloud Run..."
	./deployments/deploy.sh

# Clean
clean:
	@echo "Cleaning build artifacts..."
	rm -rf extension/dist
	rm -rf extension/node_modules
	go clean
	docker-compose down -v

# Setup
setup:
	@echo "Setting up development environment..."
	@echo "Installing extension dependencies..."
	cd extension && npm install
	@echo "Downloading Go dependencies..."
	go mod download
	@echo "Setup complete!"
