.PHONY: help build run test clean docker-up docker-down docker-clean migrate example

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "🔨 Building application..."
	@go build -o bin/market_order ./cmd/main.go
	@echo "✅ Build complete: bin/market_order"

run: ## Run the application
	@echo "🚀 Starting Market Order Service..."
	@go run cmd/main.go

test: ## Run tests
	@echo "🧪 Running tests..."
	@go test -v ./...

clean: ## Clean build artifacts
	@echo "🧹 Cleaning..."
	@rm -rf bin/
	@echo "✅ Clean complete"

docker-clean: ## Clean Docker volumes and containers
	@echo "🧹 Cleaning Docker volumes..."
	@docker-compose down -v
	@echo "✅ Docker cleaned"

docker-up: ## Start PostgreSQL and RabbitMQ via Docker Compose
	@echo "🐳 Starting Docker services..."
	@docker-compose up -d
	@echo "⏳ Waiting for services to be healthy..."
	@sleep 10
	@docker-compose ps
	@echo "✅ Docker services started"
	@echo "   PostgreSQL: localhost:5433 (user: postgres, password: postgres, db: eventstore)"
	@echo "   RabbitMQ:   localhost:5672 (user: guest, password: guest)"
	@echo "   RabbitMQ UI: http://localhost:15672"

docker-down: ## Stop Docker services
	@echo "🛑 Stopping Docker services..."
	@docker-compose down
	@echo "✅ Docker services stopped"

migrate: ## Run database migrations (run after docker-up)
	@echo "📊 Running migrations..."
	@docker exec -i market_order_postgres psql -U postgres -d eventstore < infrastructure/database/migrations.sql
	@echo "✅ Migrations complete"

example: ## Run example API calls
	@echo "📡 Running example API calls..."
	@./example_usage.sh

install: ## Install dependencies
	@echo "📦 Installing dependencies..."
	@go mod download
	@echo "✅ Dependencies installed"

dev: docker-clean docker-up migrate build run ## Start fresh development environment

all: clean install build ## Clean, install dependencies, and build

.DEFAULT_GOAL := help
