package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"market_order/api"
	"market_order/application/aggregates"
	"market_order/application/notification"
	"market_order/application/saga"
	"market_order/application/usecases"
	"market_order/infrastructure/eventstore"
	"market_order/infrastructure/idempotency"
	"market_order/infrastructure/messaging"
	"market_order/infrastructure/outbox"
	"market_order/infrastructure/repository"
)

func main() {
	log.Println("🚀 Starting Market Order Service...")

	// =====================================================
	// 1. Database Connection (with retry)
	// =====================================================
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/eventstore?sslmode=disable")

	var db *sql.DB
	var err error

	// Retry connection up to 10 times (for Docker startup)
	for i := 0; i < 3; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err != nil {
			log.Printf("⏳ Attempt %d/10: Failed to open database: %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		err = db.Ping()
		if err == nil {
			break // Success!
		}

		log.Printf("⏳ Attempt %d/10: Database ping failed: %v", i+1, err)
		db.Close()
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatalf("❌ Failed to connect to database after 10 attempts: %v", err)
	}
	defer db.Close()

	log.Println("✅ Connected to PostgreSQL")

	// =====================================================
	// 2. Infrastructure Layer
	// =====================================================

	// Event Store
	es := eventstore.NewPostgresEventStore(db)
	log.Println("✅ Event Store initialized")

	// RabbitMQ (with retry)
	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	mb := messaging.NewRabbitMQ(rabbitURL)

	for i := 0; i < 10; i++ {
		err = mb.Connect()
		if err == nil {
			break
		}
		log.Printf("⏳ Attempt %d/10: Failed to connect to RabbitMQ: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatalf("❌ Failed to connect to RabbitMQ after 10 attempts: %v", err)
	}
	defer mb.Close()

	// Idempotency
	processedEventsRepo := idempotency.NewProcessedEventsRepository(db)
	log.Println("✅ Idempotency repository initialized")

	// =====================================================
	// 3. Repositories (EventStore ONLY - source of truth)
	// =====================================================
	orderRepo := repository.NewOrderRepository(es)
	positionRepo := repository.NewPositionRepository(es)
	log.Println("✅ Repositories initialized (EventStore)")

	// =====================================================
	// 4. Aggregate Store (for commands and queries)
	// =====================================================
	aggregateStore := aggregates.NewAggregateStore(es)
	log.Println("✅ Aggregate Store initialized")

	// =====================================================
	// 5. Use Cases (using AggregateStore)
	// =====================================================
	createOrderUC := usecases.NewCreateOrderUseCase(aggregateStore)
	completeOrderAndPosUC := usecases.NewCompleteOrderAndUpdatePositionUseCase(aggregateStore)
	log.Println("✅ Use cases initialized")

	// =====================================================
	// 5. External Services (Mock for demo)
	// =====================================================
	priceService := &MockPriceService{}
	tradeWorker := &MockTradeWorker{}
	notifier := &notification.MockNotifier{}
	log.Println("✅ External services initialized (mock)")

	// =====================================================
	// 6. Saga Orchestrator (using AggregateStore)
	// =====================================================
	orderSaga := saga.NewOrderSagaRefactored(
		aggregateStore,
		processedEventsRepo,
		completeOrderAndPosUC,
		mb,
		priceService,
		tradeWorker,
	)
	log.Println("✅ Saga orchestrator initialized")

	// =====================================================
	// 7. Notification Service (using EventStore for queries)
	// =====================================================
	notificationService := notification.NewNotificationService(
		orderRepo,
		positionRepo,
		processedEventsRepo,
		mb,
		notifier,
	)
	log.Println("✅ Notification service initialized")

	// =====================================================
	// 8. Outbox Publisher (Transactional Outbox Pattern)
	// =====================================================
	outboxPub := outbox.NewOutboxPublisher(db, mb)
	log.Println("✅ Outbox publisher initialized")

	// =====================================================
	// 9. API Server
	// =====================================================
	orderHandler := api.NewOrderHandler(createOrderUC, es)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.HealthCheck)
	mux.HandleFunc("/orders", orderHandler.CreateOrder)
	mux.HandleFunc("/orders/", orderHandler.GetOrderHistory)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Println("✅ HTTP server configured on :8080")

	// =====================================================
	// 10. Start Background Workers
	// =====================================================
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Outbox Publisher (publishes events to RabbitMQ)
	go func() {
		log.Println("🔄 Starting Outbox Publisher...")
		if err := outboxPub.Start(ctx); err != nil {
			log.Printf("❌ Outbox publisher error: %v", err)
		}
	}()

	// Start Saga Orchestrator (listens to OrderAccepted events)
	go func() {
		log.Println("🔄 Starting Saga Orchestrator...")
		if err := orderSaga.Start(ctx); err != nil {
			log.Printf("❌ Saga orchestrator error: %v", err)
		}
	}()

	// Start Notification Service (listens to OrderCompleted/OrderFailed events)
	go func() {
		log.Println("🔄 Starting Notification Service...")
		if err := notificationService.Start(ctx); err != nil {
			log.Printf("❌ Notification service error: %v", err)
		}
	}()

	// Start HTTP Server
	go func() {
		log.Println("🌐 Starting HTTP server on :8080...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ HTTP server error: %v", err)
		}
	}()

	// =====================================================
	// 11. Graceful Shutdown
	// =====================================================
	log.Println("✅ All services started successfully!")
	log.Println("📡 Listening for orders on http://localhost:8080/orders")
	log.Println("Press Ctrl+C to shutdown...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("\n🛑 Shutting down gracefully...")

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ HTTP server shutdown error: %v", err)
	}

	// Cancel background workers
	cancel()

	log.Println("👋 Goodbye!")
}

// =====================================================
// Mock Implementations (for demo purposes)
// =====================================================

type MockPriceService struct{}

func (m *MockPriceService) GetMarketPrice(ctx context.Context, from, to string) (float64, error) {
	// Simulate price service
	log.Printf("💰 [MockPriceService] Getting price for %s/%s", from, to)

	// Simulate prices
	if from == "USDT" && to == "BTC" {
		return 100000.0, nil // 1 BTC = 100k USDT
	}
	if from == "USDT" && to == "ETH" {
		return 4000.0, nil // 1 ETH = 4k USDT
	}

	return 1.0, nil // Default
}

type MockTradeWorker struct{}

func (m *MockTradeWorker) ExecuteSwap(ctx context.Context, req saga.SwapRequest) (*saga.SwapResponse, error) {
	// Simulate swap execution
	log.Printf("🔄 [MockTradeWorker] Executing swap: %.2f %s -> %s (idempotency: %s)",
		req.FromAmount, req.FromCurrency, req.ToCurrency, req.IdempotencyKey)

	// Simulate network delay
	time.Sleep(100 * time.Millisecond)

	// Calculate toAmount based on mock prices
	var price float64
	if req.FromCurrency == "USDT" && req.ToCurrency == "BTC" {
		price = 100000.0
	} else if req.FromCurrency == "USDT" && req.ToCurrency == "ETH" {
		price = 4000.0
	} else {
		price = 1.0
	}

	toAmount := req.FromAmount / price

	return &saga.SwapResponse{
		TransactionHash: "0xabc123def456789...",
		ToAmount:        toAmount,
		ExecutedPrice:   price,
		Fees:            0.5,  // 0.5 USDT
		Slippage:        0.02, // 0.02%
	}, nil
}

// Helper function
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
