# Project Structure

```
saga_market_order/
├── README.md                          # Main documentation
├── PROJECT_STRUCTURE.md               # This file
├── Makefile                           # Build automation
├── docker-compose.yml                 # Docker services (Postgres, RabbitMQ)
├── example_usage.sh                   # Example API calls
├── go.mod                             # Go dependencies
├── go.sum                             # Go dependency checksums
│
├── cmd/
│   └── main.go                        # Application entry point
│                                      # - Wires all dependencies
│                                      # - Starts HTTP server
│                                      # - Starts background workers
│
├── domain/                            # Domain layer (DDD)
│   ├── order/
│   │   ├── aggregate.go              # Order aggregate
│   │   │                             # - AcceptOrder()
│   │   │                             # - QuotePrice()
│   │   │                             # - StartSwapExecution()
│   │   │                             # - RecordSwapExecution()
│   │   │                             # - CompleteOrder()
│   │   │                             # - FailOrder()
│   │   ├── events.go                 # Order events
│   │   │                             # - OrderAccepted
│   │   │                             # - PriceQuoted
│   │   │                             # - SwapExecuting
│   │   │                             # - SwapExecuted
│   │   │                             # - OrderCompleted
│   │   │                             # - OrderFailed
│   │   └── uuid.go                   # UUID helper
│   │
│   └── position/
│       ├── aggregate.go              # Position aggregate
│       │                             # - CreatePosition()
│       │                             # - AddOrder()
│       │                             # - ClosePosition()
│       ├── events.go                 # Position events
│       │                             # - PositionCreated
│       │                             # - PositionUpdated
│       │                             # - PositionClosed
│       └── uuid.go                   # UUID helper
│
├── application/                       # Application layer
│   ├── usecases/
│   │   ├── create_order.go           # Create order use case
│   │   │                             # - Accepts order
│   │   │                             # - Saves to Event Store
│   │   └── complete_order_and_update_position.go
│   │                                 # - Completes order
│   │                                 # - Updates position
│   │                                 # - ATOMIC transaction (critical!)
│   │
│   ├── saga/
│   │   └── order_saga.go             # Saga orchestrator
│   │                                 # - Listens to OrderAccepted
│   │                                 # - Executes workflow:
│   │                                 #   1. Get price
│   │                                 #   2. Create position
│   │                                 #   3. Execute swap
│   │                                 #   4. Complete order + update position
│   │                                 # - Handles failures (compensation)
│   │                                 # - Idempotency via processed_events
│   │
│   └── notification/
│       └── service.go                # Notification service
│                                     # - Listens to OrderCompleted/OrderFailed
│                                     # - Sends notifications (Telegram, etc.)
│                                     # - Idempotency via processed_events
│
├── infrastructure/                    # Infrastructure layer
│   ├── eventstore/
│   │   ├── postgres.go               # Event Store implementation
│   │   │                             # - Save() - atomic save to events + outbox
│   │   │                             # - Load() - rebuild aggregate from events
│   │   │                             # - Optimistic locking (version constraint)
│   │   └── serializer.go             # Event serialization
│   │                                 # - serializeEvent()
│   │                                 # - BaseFieldsProvider interface
│   │
│   ├── repository/
│   │   ├── order_repository.go       # Order repository
│   │   │                             # - Get() - load from Event Store
│   │   │                             # - Save() - save events
│   │   └── position_repository.go    # Position repository
│   │                                 # - Get() - load from Event Store
│   │                                 # - Save() - save events
│   │
│   ├── messaging/
│   │   └── rabbitmq.go               # RabbitMQ client
│   │                                 # - Connect()
│   │                                 # - Publish() - send events
│   │                                 # - Subscribe() - consume events
│   │
│   ├── outbox/
│   │   └── publisher.go              # Outbox publisher (background worker)
│   │                                 # - Polls outbox table
│   │                                 # - Publishes to RabbitMQ
│   │                                 # - Marks as published
│   │                                 # - Runs every 100ms
│   │
│   ├── idempotency/
│   │   └── processed_events.go       # Idempotency repository
│   │                                 # - IsProcessed() - check if event processed
│   │                                 # - MarkAsProcessed() - mark as processed
│   │
│   └── database/
│       └── migrations.sql            # Database schema
│                                     # - events table
│                                     # - outbox table
│                                     # - processed_events table
│                                     # - saga_state table (optional)
│                                     # - Read models (order_view, position_view)
│
├── api/
│   └── handlers.go                   # HTTP handlers
│                                     # - POST /orders - create order
│                                     # - GET /health - health check
│
└── pkg/
    └── uuid/
        └── uuid.go                   # UUID utilities
                                      # - New() - generate UUID
                                      # - Parse() - parse UUID
```

---

## Event Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                         HTTP Request                            │
│  POST /orders                                                   │
│  {                                                              │
│    "user_id": "user-123",                                       │
│    "from_amount": 1000,                                         │
│    "from_currency": "USDT",                                     │
│    "to_currency": "BTC",                                        │
│    "order_type": "market"                                       │
│  }                                                              │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    OrderHandler.CreateOrder()                   │
│  • Validates request                                            │
│  • Generates orderID                                            │
│  • Calls CreateOrderUseCase                                     │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                  CreateOrderUseCase.Execute()                   │
│  • order = NewOrder()                                           │
│  • order.AcceptOrder(...)                                       │
│  • → Generates OrderAccepted event (version=1)                  │
│  • orderRepo.Save(order)                                        │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                 OrderRepository.Save(order)                     │
│  • eventStore.Save(order.Changes)                               │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│              PostgresEventStore.Save([events])                  │
│  BEGIN TRANSACTION                                              │
│    1. INSERT INTO events (OrderAccepted, version=1)             │
│    2. INSERT INTO outbox (OrderAccepted, published=false)       │
│  COMMIT                                                         │
│                                                                 │
│  ✅ ATOMIC: Either both saved or nothing                        │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│          OutboxPublisher (background worker, 100ms poll)        │
│  1. SELECT * FROM outbox WHERE published=false                  │
│  2. FOR EACH event:                                             │
│     - rabbitmq.Publish(event)                                   │
│     - UPDATE outbox SET published=true                          │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                         RabbitMQ                                │
│  Exchange: "events" (type: topic)                               │
│  Routing Key: "OrderAccepted"                                   │
└───────────────┬────────────────────────────┬────────────────────┘
                │                            │
        ┌───────┴────────┐          ┌────────┴──────────┐
        │                │          │                   │
        ▼                ▼          ▼                   ▼
  ┌─────────┐      ┌─────────┐  ┌──────────┐     ┌──────────┐
  │  Saga   │      │  Saga   │  │ Notif.   │     │ Notif.   │
  │ Queue:  │      │Consumer │  │ Queue:   │     │ Consumer │
  │OrderAccepted   │         │  │OrderCompleted  │          │
  └─────────┘      └────┬────┘  └──────────┘     └──────────┘
                        │
                        ▼
        ┌───────────────────────────────────────────┐
        │  OrderSaga.handleOrderAccepted()          │
        │                                           │
        │  1. IDEMPOTENCY CHECK                     │
        │     - IsProcessed(eventID)?               │
        │     - Yes → SKIP, No → Continue           │
        │                                           │
        │  2. Get Market Price                      │
        │     - priceService.GetMarketPrice()       │
        │     - order.QuotePrice(price, toAmount)   │
        │     - Save → PriceQuoted event            │
        │                                           │
        │  3. Create Position                       │
        │     - position.CreatePosition()           │
        │     - Save → PositionCreated event        │
        │                                           │
        │  4. Execute Swap                          │
        │     - order.StartSwapExecution()          │
        │     - tradeWorker.ExecuteSwap()           │
        │     - order.RecordSwapExecution()         │
        │     - Save → SwapExecuted event           │
        │                                           │
        │  5. Complete Order + Update Position      │
        │     - completeOrderUC.Execute()           │
        │       → OrderCompleted + PositionUpdated  │
        │       → SAVED IN ONE TRANSACTION!         │
        │                                           │
        │  6. Mark as processed                     │
        │     - processedEvents.MarkAsProcessed()   │
        │                                           │
        │  ✅ SAGA COMPLETED                        │
        └───────────────────────────────────────────┘
```

---

## Key Components

### 1. Event Store
- **File**: `infrastructure/eventstore/postgres.go`
- **Responsibility**: Persist events, implement Transactional Outbox
- **Guarantees**: ACID, Optimistic Locking

### 2. Saga Orchestrator
- **File**: `application/saga/order_saga.go`
- **Responsibility**: Orchestrate order execution workflow
- **Features**: Idempotency, Compensation, Step-by-step execution

### 3. Outbox Publisher
- **File**: `infrastructure/outbox/publisher.go`
- **Responsibility**: Publish events to RabbitMQ reliably
- **Pattern**: Transactional Outbox Pattern

### 4. Notification Service
- **File**: `application/notification/service.go`
- **Responsibility**: Send notifications on order completion/failure
- **Features**: Idempotency, Multiple channels support

### 5. Use Cases
- **CreateOrderUseCase**: Accept new orders
- **CompleteOrderAndUpdatePositionUseCase**: Complete order + update position atomically

---

## Database Tables

### events
```sql
CREATE TABLE events (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,
    aggregate_id UUID NOT NULL,
    aggregate_type VARCHAR(50) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    event_data JSONB NOT NULL,
    metadata JSONB,
    version INT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(aggregate_id, version)  -- Optimistic Locking
);
```

### outbox
```sql
CREATE TABLE outbox (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,
    aggregate_id UUID NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    event_data JSONB NOT NULL,
    published BOOLEAN DEFAULT FALSE,
    published_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);
```

### processed_events
```sql
CREATE TABLE processed_events (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,
    aggregate_id UUID NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    processed_at TIMESTAMP DEFAULT NOW(),
    processed_by VARCHAR(100)
);
```

---

## Running the Application

```bash
# 1. Start dependencies (Postgres + RabbitMQ)
make docker-up

# 2. Run application
make run

# 3. Create order
make example
```
