# Market Order SAGA - Event Sourcing + CQRS Implementation

A production-ready implementation of the **Orchestrated Saga pattern** with **Event Sourcing** and **CQRS** for handling market order swap operations.

## 🏗️ Architecture Overview

```
┌─────────────┐
│   HTTP API  │ POST /orders
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│                    Use Case Layer                           │
│  ┌───────────────────────────────────────────────────────┐ │
│  │ CreateOrderUseCase                                     │ │
│  │  • Creates Order aggregate                             │ │
│  │  • Generates OrderAccepted event                       │ │
│  │  • Saves to Event Store + Outbox (atomic)             │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│                 Event Store (PostgreSQL)                     │
│  ┌─────────────┐  ┌──────────────┐                         │
│  │   events    │  │    outbox    │                         │
│  │  table      │  │    table     │                         │
│  └─────────────┘  └──────────────┘                         │
│         Atomic Transaction (ACID guaranteed)                │
└─────────────────────────────────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│               Outbox Publisher (Background)                  │
│  • Polls outbox table every 100ms                           │
│  • Publishes events to RabbitMQ                             │
│  • Marks as published                                       │
└─────────────────────────────────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│                    RabbitMQ (Event Bus)                      │
│  Exchange: "events" (topic)                                 │
│  Routing Keys: OrderAccepted, OrderCompleted, etc.         │
└─────────────────────────────────────────────────────────────┘
       │
       ├──────────────────────────────────────────┐
       │                                          │
       ▼                                          ▼
┌──────────────────┐                  ┌──────────────────────┐
│  Saga Orchestrator│                 │ Notification Service │
│                   │                 │                      │
│ Listens:          │                 │ Listens:             │
│  • OrderAccepted  │                 │  • OrderCompleted    │
│                   │                 │  • OrderFailed       │
│ Workflow:         │                 │                      │
│ 1. Get Price      │                 │ Sends:               │
│ 2. Create Position│                 │  • Telegram msg      │
│ 3. Execute Swap   │                 │  • Email (optional)  │
│ 4. Complete Order │                 │                      │
│    + Update Pos   │                 │ Idempotency:         │
│    (ATOMIC!)      │                 │  • processed_events  │
│                   │                 │                      │
│ Idempotency:      │                 │                      │
│  • processed_events│                │                      │
│                   │                 │                      │
│ Compensation:     │                 │                      │
│  • OrderFailed    │                 │                      │
│  • PositionClosed │                 │                      │
└──────────────────┘                 └──────────────────────┘
```

---

## 📋 SAGA Sequence Diagram

```
User → API → CreateOrderUC → Event Store → Outbox → RabbitMQ → Saga
                                                              │
                                                              ▼
                                                     ┌────────────────┐
                                                     │ OrderAccepted  │
                                                     └────────┬───────┘
                                                              │
                    ┌─────────────────────────────────────────┼─────────────────────────┐
                    │ IDEMPOTENCY CHECK                       │                         │
                    │ SELECT FROM processed_events            │                         │
                    │ WHERE event_id = ?                      │                         │
                    │ → If found: SKIP                        │                         │
                    └─────────────────────────────────────────┼─────────────────────────┘
                                                              │
                    ┌─────────────────────────────────────────▼─────────────────────────┐
                    │ STEP 1: Get Market Price                                          │
                    │  • Call PriceService.GetMarketPrice(USDT, BTC)                    │
                    │  • price = 100000, toAmount = 1000/100000 = 0.01                  │
                    │  • Generate PriceQuoted event                                     │
                    │  • Save to Event Store                                             │
                    └─────────────────────────────────────────┬─────────────────────────┘
                                                              │
                    ┌─────────────────────────────────────────▼─────────────────────────┐
                    │ STEP 2: Create Position                                           │
                    │  • position = NewPosition()                                       │
                    │  • position.CreatePosition(positionID, userID)                    │
                    │  • Generate PositionCreated event                                 │
                    │  • Save to Event Store                                             │
                    └─────────────────────────────────────────┬─────────────────────────┘
                                                              │
                    ┌─────────────────────────────────────────▼─────────────────────────┐
                    │ STEP 3: Execute Swap                                              │
                    │  • order.StartSwapExecution(idempotencyKey)                       │
                    │  • Generate SwapExecuting event                                   │
                    │  • Call TradeWorker.ExecuteSwap(...)                              │
                    │  • txHash = "0xabc...", toAmount = 0.01                           │
                    │  • Generate SwapExecuted event                                    │
                    └─────────────────────────────────────────┬─────────────────────────┘
                                                              │
                    ┌─────────────────────────────────────────▼─────────────────────────┐
                    │ STEP 4: Complete Order + Update Position (ATOMIC)                 │
                    │                                                                    │
                    │  CompleteOrderAndUpdatePositionUseCase:                           │
                    │  ┌──────────────────────────────────────────────────────────────┐ │
                    │  │ 1. Load Order aggregate                                       │ │
                    │  │ 2. order.CompleteOrder()                                      │ │
                    │  │    → Generate OrderCompleted event                            │ │
                    │  │                                                               │ │
                    │  │ 3. Load Position aggregate                                    │ │
                    │  │ 4. position.AddOrder(orderID, toAmount, ...)                  │ │
                    │  │    → Generate PositionUpdated event                           │ │
                    │  │                                                               │ │
                    │  │ 5. Save BOTH events in ONE transaction:                       │ │
                    │  │    eventStore.Save([OrderCompleted, PositionUpdated])         │ │
                    │  │    → PostgreSQL BEGIN                                         │ │
                    │  │    → INSERT INTO events (OrderCompleted, version=5)           │ │
                    │  │    → INSERT INTO events (PositionUpdated, version=2)          │ │
                    │  │    → INSERT INTO outbox (2 rows)                              │ │
                    │  │    → COMMIT                                                   │ │
                    │  │                                                               │ │
                    │  │ GUARANTEES:                                                   │ │
                    │  │  ✅ Atomicity: Both events saved or none                      │ │
                    │  │  ✅ Consistency: Versions incremented                         │ │
                    │  │  ✅ Optimistic Locking: UNIQUE(aggregate_id, version)         │ │
                    │  └──────────────────────────────────────────────────────────────┘ │
                    └─────────────────────────────────────────┬─────────────────────────┘
                                                              │
                    ┌─────────────────────────────────────────▼─────────────────────────┐
                    │ Mark event as processed                                           │
                    │  INSERT INTO processed_events (event_id, aggregate_id, ...)       │
                    └─────────────────────────────────────────┬─────────────────────────┘
                                                              │
                                                              ▼
                                                        ✅ SAGA COMPLETED
```

---

## 🔐 Idempotency Guarantees

### 3 Levels of Idempotency Protection

```
┌─────────────────────────────────────────────────────────────┐
│ 1. EVENT-LEVEL IDEMPOTENCY                                  │
├─────────────────────────────────────────────────────────────┤
│ Table: processed_events                                     │
│ Key: event_id (UUID)                                        │
│                                                             │
│ func HandleEvent(event) {                                   │
│   if IsProcessed(event.EventID) {                          │
│     log("Already processed, skipping")                     │
│     return nil                                             │
│   }                                                        │
│   // Process event...                                       │
│   MarkAsProcessed(event.EventID)                           │
│ }                                                          │
│                                                             │
│ ✅ Prevents duplicate processing of same event             │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ 2. AGGREGATE-LEVEL IDEMPOTENCY (Optimistic Locking)         │
├─────────────────────────────────────────────────────────────┤
│ Table: events                                               │
│ Constraint: UNIQUE(aggregate_id, version)                   │
│                                                             │
│ Example:                                                    │
│  Process A: Tries to save Order version 1→2                │
│  Process B: Tries to save Order version 1→2 (race!)        │
│                                                             │
│  Process A: INSERT version=2 → SUCCESS ✅                   │
│  Process B: INSERT version=2 → CONFLICT ❌                  │
│            → Reload Order (now version=2)                   │
│            → Check status: already "completed"              │
│            → SKIP (idempotent)                             │
│                                                             │
│ ✅ Prevents version conflicts and race conditions          │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ 3. BUSINESS-LEVEL IDEMPOTENCY                               │
├─────────────────────────────────────────────────────────────┤
│ func CompleteOrder() {                                      │
│   if order.Status == "completed" {                         │
│     log("Order already completed")                         │
│     return nil  // Idempotent                              │
│   }                                                        │
│   // Continue with completion...                            │
│ }                                                          │
│                                                             │
│ ✅ Business logic checks prevent invalid state transitions │
└─────────────────────────────────────────────────────────────┘
```

---

## 🚀 Getting Started

### Prerequisites

- Go 1.23+
- PostgreSQL 14+
- RabbitMQ 3.12+

### Setup

1. **Clone repository**
```bash
git clone <repo-url>
cd saga_market_order
```

2. **Install dependencies**
```bash
go mod download
```

3. **Setup PostgreSQL**
```bash
createdb eventstore
psql -d eventstore -f infrastructure/database/migrations.sql
```

4. **Start RabbitMQ** (Docker)
```bash
docker run -d --name rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  rabbitmq:3-management
```

5. **Run application**
```bash
go run cmd/main.go
```

---

## 📡 API Usage

### Create Order

```bash
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-123",
    "from_amount": 1000,
    "from_currency": "USDT",
    "to_currency": "BTC",
    "order_type": "market"
  }'
```

**Response:**
```json
{
  "order_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "message": "Order accepted and will be processed asynchronously"
}
```

### Check Health

```bash
curl http://localhost:8080/health
```

---

## 📊 Database Schema

### Events Table
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
    UNIQUE(aggregate_id, version)  -- Optimistic locking
);
```

### Outbox Table
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

### Processed Events Table
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

## 🔍 Event Flow Example

```
1. User creates order via API
   ↓
2. CreateOrderUseCase generates OrderAccepted event
   ↓
3. Event Store saves:
   - events table: OrderAccepted (version=1)
   - outbox table: OrderAccepted (published=false)
   ↓
4. Outbox Publisher publishes to RabbitMQ
   ↓
5. Saga Orchestrator receives OrderAccepted
   ↓
6. Saga executes workflow:
   a) Get price → PriceQuoted event
   b) Create position → PositionCreated event
   c) Execute swap → SwapExecuted event
   d) Complete order + update position (atomic) → OrderCompleted + PositionUpdated
   ↓
7. Notification Service receives OrderCompleted
   ↓
8. Sends Telegram notification
   ↓
9. ✅ DONE
```

---

## 🛡️ Failure Handling

### Scenario: Swap Execution Fails

```
Saga detects swap failure
  ↓
Compensation workflow:
  1. order.FailOrder(reason)
     → Generate OrderFailed event
  2. position.ClosePosition("order_failed")
     → Generate PositionClosed event
  ↓
Notification Service sends failure notification
  ↓
User receives: "Order failed: insufficient_liquidity"
```

---

## 📚 Key Patterns Used

1. **Event Sourcing**: All state changes are events
2. **CQRS**: Separate write (commands) and read (queries) models
3. **Orchestrated Saga**: Centralized saga orchestrator
4. **Transactional Outbox**: Guarantees event publishing
5. **Optimistic Locking**: Version-based concurrency control
6. **Idempotency**: Three levels of protection
7. **Domain-Driven Design**: Aggregates, Events, Use Cases

---

## 🎯 Interview Talking Points

1. **Atomicity**: Events + Outbox saved in one DB transaction
2. **Consistency**: Optimistic locking via `UNIQUE(aggregate_id, version)`
3. **Idempotency**: Event-level, aggregate-level, business-level checks
4. **Ordering**: Events replayed by version ASC
5. **Durability**: PostgreSQL ACID + RabbitMQ persistence
6. **Compensation**: Saga handles failures with compensating transactions
7. **Scalability**: Async event processing, horizontal scaling possible

---

## 📝 License

MIT
