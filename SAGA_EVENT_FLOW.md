# Saga Event Flow Documentation

## Event Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│ USER REQUEST: Create Market Order                                       │
└──────────────────────┬──────────────────────────────────────────────────┘
                       │
                       ▼
        ┌──────────────────────────────┐
        │ OrderAccepted Event Published│
        │ (order_saga.go:81)          │
        └──────────┬───────────────────┘
                   │
                   ▼
        ┌──────────────────────────────┐
        │ handleOrderAccepted Listener │
        │ (order_saga.go:92-225)      │
        └──────────┬───────────────────┘
                   │
                   ├─► [IDEMPOTENCY CHECK]
                   │   ✓ Check if EventID already processed
                   │   ✓ Skip if already handled
                   │
                   ├─► [STEP 1: Price Quote]
                   │   CMD: GetMarketPrice()
                   │   EVENT: PriceQuoted
                   │   AGGREGATE: Order (v2)
                   │
                   ├─► [STEP 2: Create Position]
                   │   CMD: CreatePosition()
                   │   EVENT: PositionCreated
                   │   AGGREGATE: Position (v1)
                   │
                   ├─► [STEP 3: Execute Swap]
                   │   CMD: ExecuteSwap()
                   │   EVENTS: SwapExecuting → SwapExecuted
                   │   AGGREGATE: Order (v3, v4)
                   │
                   └─► [STEP 4: Complete Order & Update Position]
                       USECASE: CompleteOrderAndUpdatePositionUseCase
                       EVENTS: OrderCompleted + PositionUpdated
                       AGGREGATES: Order (v5) + Position (v2)
                       ✓ ATOMIC TRANSACTION (both or nothing)
                       ✓ Mark EventID as processed
```

---

## Detailed Event Flow

### 1. OrderAccepted Event
**Event Name:** `OrderAccepted`

**Listener:** `handleOrderAccepted` ([order_saga.go:92](application/saga/order_saga.go#L92))

**What the Listener Does:**
- Checks idempotency using `ProcessedEventsRepository`
- Orchestrates 4-step saga workflow
- Calls external services (PriceService, TradeWorker)
- Executes use cases (CompleteOrderAndUpdatePositionUseCase)

**Aggregates Updated:**
- **Order Aggregate** (multiple times during saga)
- **Position Aggregate** (created and updated)

**Idempotency Guarantee:**
```go
// Check if event already processed
processed, err := s.processedEvents.IsProcessed(ctx, evt.EventID)
if processed {
    return nil // Skip duplicate
}

// ... saga execution ...

// Mark as processed at the end
s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga")
```

**Resulting Events:**
- `PriceQuoted`
- `PositionCreated`
- `SwapExecuting`
- `SwapExecuted`
- `OrderCompleted`
- `PositionUpdated`

---

### 2. PriceQuoted Event
**Event Name:** `PriceQuoted` ([order/events.go:62](domain/order/events.go#L62))

**Triggered By:** `Order.QuotePrice()` method ([order_saga.go:132](application/saga/order_saga.go#L132))

**What Happens:**
- Order aggregate records market price and calculated toAmount
- Order state: `pending` → `pending` (status unchanged, but enriched with price data)

**Aggregate Updated:** Order (version incremented: v1 → v2)

**Idempotency:**
Event sourcing ensures version control. Duplicate events rejected by optimistic locking.

---

### 3. PositionCreated Event
**Event Name:** `PositionCreated` ([position/events.go:29](domain/position/events.go#L29))

**Triggered By:** `Position.CreatePosition()` method ([order_saga.go:146](application/saga/order_saga.go#L146))

**What Happens:**
- New position aggregate created for user
- Initial state: `status=open`, `remainingAmount=0`

**Aggregate Updated:** Position (version: v1)

**Idempotency:**
Position created with unique UUID. If saga retries, idempotency check prevents duplicate creation.

---

### 4. SwapExecuting → SwapExecuted Events
**Event Names:** `SwapExecuting`, `SwapExecuted` ([order/events.go:74-92](domain/order/events.go#L74))

**Triggered By:**
1. `Order.StartSwapExecution()` → `SwapExecuting` ([order_saga.go:163](application/saga/order_saga.go#L163))
2. `Order.RecordSwapExecution()` → `SwapExecuted` ([order_saga.go:186](application/saga/order_saga.go#L186))

**What Happens:**
- CMD: `TradeWorker.ExecuteSwap()` with idempotency key
- Order records swap transaction hash, executed price, fees, slippage
- External swap executed on blockchain/DEX

**Aggregate Updated:** Order (v3 → v4)

**Idempotency:**
```go
idempotencyKey := fmt.Sprintf("swap-%s", orderID)
swapReq := SwapRequest{IdempotencyKey: idempotencyKey, ...}
```
TradeWorker must implement idempotent swap execution using this key.

---

### 5. OrderCompleted + PositionUpdated (ATOMIC)
**Event Names:** `OrderCompleted`, `PositionUpdated`

**Triggered By:** `CompleteOrderAndUpdatePositionUseCase.Execute()` ([order_saga.go:199](application/saga/order_saga.go#L199))

**What the UseCase Does:**
1. Loads Order aggregate → calls `CompleteOrder()` → generates `OrderCompleted` event
2. Loads Position aggregate → calls `AddOrder()` → generates `PositionUpdated` event
3. **Saves both events in SINGLE transaction** ([complete_order_and_update_position.go:78](application/usecases/complete_order_and_update_position.go#L78))

```go
allEvents := append(o.Changes, p.Changes...)
eventStore.Save(ctx, allEvents) // ATOMIC
```

**Aggregates Updated:**
- **Order:** v4 → v5 (status: `executing` → `completed`)
- **Position:** v1 → v2 (adds order to position, updates remainingAmount, PnL)

**Idempotency Guarantee:**
- Event Store ensures atomicity: both aggregates updated or neither
- If saga retries after swap but before completion, idempotency check prevents re-execution
- Event ID marked as processed AFTER successful atomic save

---

## Aggregate State Changes

### Order Aggregate
```
v1: OrderAccepted      (status: pending)
v2: PriceQuoted        (price added)
v3: SwapExecuting      (status: executing)
v4: SwapExecuted       (swap details recorded)
v5: OrderCompleted     (status: completed)
```

### Position Aggregate
```
v1: PositionCreated    (status: open, remainingAmount: 0)
v2: PositionUpdated    (order added, remainingAmount: 0.01btc)
```

---

## Summary Table

| Event             | Handler/CMD                        | Aggregates Updated | Idempotency Method           |
|-------------------|------------------------------------|--------------------|------------------------------|
| OrderAccepted     | handleOrderAccepted (saga)         | Order, Position    | ProcessedEvents check        |
| PriceQuoted       | Order.QuotePrice()                 | Order (v2)         | Event sourcing versioning    |
| PositionCreated   | Position.CreatePosition()          | Position (v1)      | Event sourcing versioning    |
| SwapExecuting     | Order.StartSwapExecution()         | Order (v3)         | Event sourcing versioning    |
| SwapExecuted      | Order.RecordSwapExecution()        | Order (v4)         | Idempotency key on swap      |
| OrderCompleted    | CompleteOrderAndUpdatePositionUC   | Order (v5)         | Event Store transaction      |
| PositionUpdated   | CompleteOrderAndUpdatePositionUC   | Position (v2)      | Event Store transaction      |
