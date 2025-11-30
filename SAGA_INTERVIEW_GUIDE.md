# SAGA Pattern Interview Guide

## Quick Reference: Market Order Flow

### Complete Event Flow

```
HTTP POST /orders 
  ↓
OrderAccepted 
  ↓ (Saga listens)
PriceQuoted 
  ↓ (Saga continues)
PositionCreated 
  ↓ (Saga continues)
SwapExecuted 
  ↓ (Saga calls UseCase)
OrderCompleted + PositionUpdated (ATOMIC!)
  ↓ (NotificationService listens)
NotificationSent
```

---

## Step-by-Step Breakdown

### Step 1: OrderAccepted

**1. Event Name:** `OrderAccepted`

**2. What Listener Does:**
- **Listener:** Saga Orchestrator (subscribed to RabbitMQ)
- **Action:** Calls `order.QuotePrice(price, toAmount)` command
- **Flow:**
  ```
  1. Check idempotency (processed_events table)
  2. Get market price from PriceService
  3. Update Order aggregate with price
  4. Save to Event Store
  ```

**3. Aggregate Updates:**
```
Order v1 → v2
BEFORE: {status: "pending", toAmount: null, version: 1}
AFTER:  {status: "pending", toAmount: 0.01, version: 2}
```

**3.1 Idempotency:**
- ✅ Event-level: `SELECT FROM processed_events WHERE event_id = ?`
- ✅ Optimistic locking: `UNIQUE(aggregate_id, version)`
- ✅ Business logic: `if status != "pending" return error`

**4. Exiting Events:** `PriceQuoted`

---

### Step 2: PositionCreated

**1. Event Name:** `PriceQuoted` (triggers this step)

**2. What Listener Does:**
- **Listener:** Saga Orchestrator (continues in same handler)
- **Action:** Calls `position.CreatePosition()` command
- **Flow:**
  ```
  1. Create new Position aggregate
  2. Generate PositionCreated event
  3. Save to Event Store
  ```

**3. Aggregate Updates:**
```
Position v0 → v1 (NEW AGGREGATE)
AFTER: {
  id: "pos_789",
  orders: [],
  remainingAmount: 0,
  status: "open",
  version: 1
}
```

**3.1 Idempotency:**
- ✅ Aggregate doesn't exist yet (new creation)
- ✅ Optimistic locking prevents duplicates
- ✅ If retry, `Get()` will find existing position

**4. Exiting Events:** `PositionCreated`

---

### Step 3: SwapExecuted

**1. Event Name:** `SwapExecuted`

**2. What Listener Does:**
- **Listener:** Saga Orchestrator
- **Action:** Calls `order.RecordSwapExecution()` command
- **Flow:**
  ```
  1. Call TradeWorker.ExecuteSwap() (external API)
  2. Receive transaction hash
  3. Update Order with swap result
  4. Save to Event Store
  ```

**3. Aggregate Updates:**
```
Order v2 → v3
BEFORE: {status: "pending", version: 2}
AFTER:  {status: "executing", version: 3}
```

**3.1 Idempotency:**
- ✅ External API uses idempotency key: `"swap-{orderID}"`
- ✅ Retry returns same transaction hash
- ✅ Optimistic locking on event save

**4. Exiting Events:** `SwapExecuted`

---

### Step 4: OrderCompleted + PositionUpdated ⚡ ATOMIC

**1. Event Names:** 
- `OrderCompleted`
- `PositionUpdated`

**2. What Listener Does:**
- **Listener:** Saga Orchestrator
- **Action:** Calls **`CompleteOrderAndUpdatePositionUseCase`**
- **Flow:**
  ```go
  UseCase:
    1. Load Order aggregate
    2. order.CompleteOrder() → generates OrderCompleted event
    3. Load Position aggregate  
    4. position.AddOrder() → generates PositionUpdated event
    5. ⚡ Save BOTH events in ONE transaction:
       
       allEvents = [OrderCompleted, PositionUpdated]
       eventStore.Save(allEvents)
       
       PostgreSQL:
         BEGIN
           INSERT events (OrderCompleted, version=4)
           INSERT events (PositionUpdated, version=2)
           INSERT outbox (2 rows)
         COMMIT
  ```

**3. Aggregate Updates:**

**TWO aggregates updated atomically:**

```
Order v3 → v4
BEFORE: {status: "executing", version: 3}
AFTER:  {status: "completed", version: 4}

Position v1 → v2
BEFORE: {
  orders: [],
  remainingAmount: 0,
  version: 1
}
AFTER: {
  orders: [{orderId: "ord_123", amount: 0.01}],
  remainingAmount: 0.01,
  version: 2
}
```

**3.1 Idempotency:**

**CRITICAL:** Single transaction guarantees atomicity

```sql
BEGIN TRANSACTION;

-- Event 1
INSERT INTO events (aggregate_id, version, event_type)
VALUES ('ord_123', 4, 'OrderCompleted');
-- CONSTRAINT: UNIQUE(ord_123, 4) ✅

-- Event 2  
INSERT INTO events (aggregate_id, version, event_type)
VALUES ('pos_789', 2, 'PositionUpdated');
-- CONSTRAINT: UNIQUE(pos_789, 2) ✅

-- Outbox for both
INSERT INTO outbox VALUES 
  ('evt_005', 'OrderCompleted', ...),
  ('evt_006', 'PositionUpdated', ...);

COMMIT; -- ⚡ ALL or NOTHING!
```

**Race Condition Protection:**
```
Process A: Save Order v4 + Position v2 → COMMIT ✅
Process B: Save Order v4 + Position v2 → CONFLICT (version 4 exists) ❌
          → ROLLBACK
          → Reload aggregates (already completed)
          → SKIP
```

**4. Exiting Events:** 
- `OrderCompleted`
- `PositionUpdated`

---

### Step 5: NotificationSent

**1. Event Name:** `OrderCompleted` (triggers this step)

**2. What Listener Does:**
- **Listener:** Notification Service (subscribed to RabbitMQ)
- **Action:** Sends Telegram notification
- **Flow:**
  ```
  1. Check idempotency (processed_events)
  2. Load Order for details
  3. Format message
  4. Send via Telegram Bot API
  5. Mark event as processed
  ```

**3. Aggregate Updates:**
```
NONE - This is a side effect, not business logic
(Optional: create Notification aggregate for audit)
```

**3.1 Idempotency:**
```go
notificationID = hash(orderID + "OrderCompleted")

SELECT FROM notification_log 
WHERE notification_id = ?

If exists → SKIP sending
```

**4. Exiting Events:** `NotificationSent` (optional, for audit)

---

## Interview Quick Answers

### Q: "How do you guarantee Order and Position update together?"

**A:** We use a **Use Case** that collects events from **both aggregates** and saves them in a **single PostgreSQL transaction**:

```go
allEvents = [OrderCompleted, PositionUpdated]
eventStore.Save(allEvents) // ← ATOMIC via PostgreSQL ACID
```

### Q: "What about idempotency?"

**A:** We have **3 levels** of protection:

1. **Event-level:** `processed_events` table checks `event_id`
2. **Aggregate-level:** `UNIQUE(aggregate_id, version)` constraint
3. **Business-level:** Status checks in commands (`if completed → skip`)

### Q: "How does Event Sourcing guarantee consistency?"

**A:** Through **5 pillars**:

1. **Atomicity:** PostgreSQL ACID transactions
2. **Consistency:** Optimistic locking (version conflicts)
3. **Idempotency:** 3-level protection
4. **Ordering:** Replay events `ORDER BY version ASC`
5. **Durability:** Events + Outbox saved together

### Q: "What events are published after each step?"

**A:** See table below:

| Step | Input Event | Aggregate(s) Updated | Output Event(s) |
|------|-------------|---------------------|----------------|
| 1 | `OrderAccepted` | Order v1→v2 | `PriceQuoted` |
| 2 | `PriceQuoted` | Position v0→v1 | `PositionCreated` |
| 3 | - | Order v2→v3 | `SwapExecuted` |
| 4 | `SwapExecuted` | **Order v3→v4 + Position v1→v2** | `OrderCompleted` + `PositionUpdated` |
| 5 | `OrderCompleted` | None | `NotificationSent` |

---

## Key Patterns Used

### 1. Orchestrated Saga
```
Saga Orchestrator controls the workflow:
  - Listens to events
  - Executes commands in sequence
  - Handles failures with compensation
```

### 2. Event Sourcing
```
All state changes = events:
  - Events are immutable
  - Aggregates rebuilt from events
  - Full audit trail
```

### 3. Transactional Outbox
```
Events + Outbox saved together:
  BEGIN
    INSERT INTO events
    INSERT INTO outbox (published=false)
  COMMIT
  
Background worker publishes from outbox to RabbitMQ
```

### 4. Optimistic Locking
```sql
UNIQUE(aggregate_id, version)

Concurrent updates:
  Process A: version 1→2 SUCCESS
  Process B: version 1→2 CONFLICT → Retry with version 2
```

### 5. CQRS (bonus)
```
Write: Commands → Events → Event Store
Read: Events → Projections → Read Models (order_view, position_view)
```

---

## Failure Scenarios

### Swap Failed
```
COMPENSATION:
  1. order.FailOrder(reason) → OrderFailed
  2. position.ClosePosition() → PositionClosed
  3. Notification sent: "Order failed: {reason}"
```

### Optimistic Locking Conflict
```
RETRY:
  1. Reload aggregate from Event Store
  2. Check current status
  3. If already completed → SKIP
  4. Else → Apply command again
```

---

## Code References

- **Saga Orchestrator:** [application/saga/order_saga.go](application/saga/order_saga.go)
- **Atomic Use Case:** [application/usecases/complete_order_and_update_position.go](application/usecases/complete_order_and_update_position.go)
- **Event Store:** [infrastructure/eventstore/postgres.go](infrastructure/eventstore/postgres.go)
- **Idempotency:** [infrastructure/idempotency/processed_events.go](infrastructure/idempotency/processed_events.go)

---

**Perfect for interviews!** This guide covers all aspects interviewers ask about SAGA patterns with Event Sourcing. 🚀
