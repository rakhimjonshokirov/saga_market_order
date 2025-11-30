# Order Saga - Granular Event-Driven Architecture

## File Structure

```
saga/
├── order_saga_refactored.go   # Main saga orchestrator (struct, Start(), compensation)
├── types.go                    # Shared types and interfaces
├── accept.go                   # STEP 1: Price quotation
├── price.go                    # STEP 2: Position creation
├── swap.go                     # STEP 3: Swap execution
└── complete.go                 # STEP 4: Order completion
```

## Design Principle

**Each step is isolated in its own file for:**
- ✅ Clear separation of concerns
- ✅ Easy to understand flow
- ✅ Independent testing
- ✅ Better code navigation

---

## Flow Overview

```
User creates order
        ↓
┌────────────────────────────────────────────┐
│ accept.go                                  │
│ Event: OrderAccepted                       │
│ Action: Get market price                   │
│ Output: PriceQuoted                        │
└────────────────────────────────────────────┘
        ↓
┌────────────────────────────────────────────┐
│ price.go                                   │
│ Event: PriceQuoted                         │
│ Action: Create position                    │
│ Output: PositionCreatedForOrder            │
│         (with position_id in metadata)     │
└────────────────────────────────────────────┘
        ↓
┌────────────────────────────────────────────┐
│ swap.go                                    │
│ Event: PositionCreatedForOrder             │
│ Action: Execute blockchain swap            │
│ Output: SwapExecuted                       │
│         (with position_id in metadata)     │
└────────────────────────────────────────────┘
        ↓
┌────────────────────────────────────────────┐
│ complete.go                                │
│ Event: SwapExecuted                        │
│ Action: Link position & complete order     │
│ Output: PositionLinkedToOrder              │
└────────────────────────────────────────────┘
        ↓
    Order completed ✅
```

---

## File Details

### order_saga_refactored.go
**Main orchestrator**

- Defines `OrderSagaRefactored` struct
- `Start()` method subscribes to all events
- Compensation functions
- No business logic (delegated to step files)

```go
func (s *OrderSagaRefactored) Start(ctx context.Context) error {
    s.messageBus.Subscribe("OrderAccepted", s.handleOrderAccepted)
    s.messageBus.Subscribe("PriceQuoted", s.handlePriceQuoted)
    s.messageBus.Subscribe("PositionCreatedForOrder", s.handlePositionCreated)
    s.messageBus.Subscribe("SwapExecuted", s.handleSwapExecuted)
    // ...
}
```

---

### types.go
**Shared types**

- `PriceService` interface
- `TradeWorker` interface
- `SwapRequest` / `SwapResponse` structs
- Helper functions (e.g., `generateIdempotencyKey`)

---

### accept.go
**STEP 1: Price Quotation**

**Trigger:** `OrderAccepted` event
**Duration:** ~50ms
**Scalability:** Low priority (fast step)

**Responsibilities:**
1. Get market price from `PriceService`
2. Calculate `toAmount`
3. Update order aggregate with price
4. Publish `PriceQuoted` event

**Error Handling:**
- If price service fails → Compensate: Fail order

**Code:**
```go
func (s *OrderSagaRefactored) handleOrderAccepted(ctx context.Context, eventData []byte)
```

---

### price.go
**STEP 2: Position Creation**

**Trigger:** `PriceQuoted` event
**Duration:** ~100ms
**Scalability:** Medium priority

**Responsibilities:**
1. Create new `Position` aggregate
2. Save position to repository
3. Publish `PositionCreatedForOrder` event
4. **Pass `position_id` in metadata**

**Error Handling:**
- If position creation fails → Retry or fail order

**Metadata Propagation:**
```go
Metadata: map[string]interface{}{
    "position_id": positionID,  // For next steps
}
```

**Code:**
```go
func (s *OrderSagaRefactored) handlePriceQuoted(ctx context.Context, eventData []byte)
```

---

### swap.go
**STEP 3: Swap Execution** ⚡ **CRITICAL PATH**

**Trigger:** `PositionCreatedForOrder` event
**Duration:** ~5 seconds (blockchain call)
**Scalability:** **HIGH PRIORITY** - This is the bottleneck!

**Responsibilities:**
1. Extract `position_id` from event
2. Execute blockchain swap via `TradeWorker`
3. Record swap result
4. Publish `SwapExecuted` event
5. **Propagate `position_id` in metadata**

**Error Handling:**
- If swap fails → Compensate: Fail order + Close position
- This step can be retried independently

**Performance Note:**
```
Monolithic: 10 workers total → 2 orders/sec
Granular:   50 swap workers → 10 orders/sec (5x improvement!)
```

**Code:**
```go
func (s *OrderSagaRefactored) handlePositionCreated(ctx context.Context, eventData []byte)
```

---

### complete.go
**STEP 4: Order Completion**

**Trigger:** `SwapExecuted` event
**Duration:** ~50ms
**Scalability:** Low priority (fast step)

**Responsibilities:**
1. Extract `position_id` from event metadata
2. **Atomically** complete order + update position
3. Publish `PositionLinkedToOrder` event

**Error Handling:**
⚠️ **CRITICAL:** Swap already executed on blockchain!
- Cannot compensate (swap is irreversible)
- Must retry until success
- Alert for manual intervention if repeated failures

**Atomicity:**
```go
// Use database transaction
s.completeOrderUC.Execute(ctx, orderID, positionID, swapResult)
```

**Code:**
```go
func (s *OrderSagaRefactored) handleSwapExecuted(ctx context.Context, eventData []byte)
```

---

## Metadata Propagation Pattern

Since steps are independent, we pass context via event metadata:

```go
// price.go publishes:
PositionCreatedForOrder {
    PositionID: "pos-123",
    Metadata: {
        "position_id": "pos-123"
    }
}

// swap.go receives and forwards:
SwapExecuted {
    Metadata: {
        "position_id": evt.PositionID  // From previous step
    }
}

// complete.go receives:
positionID := evt.Metadata["position_id"].(string)
```

---

## Compensation Strategy

### Early Steps (accept.go, price.go)
```
Failure → Fail order
No resources allocated yet
```

### Swap Step (swap.go)
```
Failure → Fail order + Close position
Position created but swap not executed
```

### Completion Step (complete.go)
```
Failure → RETRY (do NOT compensate!)
Swap already executed on blockchain
Must complete or alert for manual fix
```

---

## Idempotency

Each step independently checks idempotency:

```go
func (s *OrderSagaRefactored) handleXxx(ctx context.Context, eventData []byte) error {
    // Parse event
    var evt Event
    json.Unmarshal(eventData, &evt)

    // Idempotency check
    if processed, _ := s.processedEvents.IsProcessed(ctx, evt.EventID); processed {
        return nil  // Safe to skip
    }

    // Process step...

    // Mark as processed
    s.processedEvents.MarkAsProcessed(ctx, evt.EventID, ...)
}
```

---

## Testing

Each step can be tested independently:

```go
// Test accept.go
func TestHandleOrderAccepted(t *testing.T) {
    mockPriceService := &MockPriceService{}
    saga := NewOrderSagaRefactored(..., mockPriceService, ...)

    event := order.OrderAccepted{...}
    err := saga.handleOrderAccepted(ctx, marshal(event))

    assert.NoError(t, err)
    assert.Equal(t, 1, mockPriceService.CallCount)
}
```

---

## Monitoring

Each file should emit its own metrics:

```go
// accept.go
metrics.Increment("saga.step1.price_quoted")
metrics.Histogram("saga.step1.duration_ms", duration)

// swap.go (most important!)
metrics.Increment("saga.step3.swaps_executed")
metrics.Histogram("saga.step3.swap_duration_ms", duration)
metrics.Increment("saga.step3.swap_failures")
```

---

## Comparison with Monolithic

### Before
```
order_saga.go
├── handleOrderAccepted()
│   ├── Get price
│   ├── Create position
│   ├── Execute swap
│   └── Complete order
```
❌ 400 lines in one function
❌ Hard to test
❌ Can't scale steps independently

### After
```
accept.go      (70 lines) - Price quotation
price.go       (85 lines) - Position creation
swap.go        (115 lines) - Swap execution
complete.go    (90 lines) - Order completion
```
✅ Clear separation
✅ Easy to test
✅ Independent scaling
✅ Better maintainability

---

## Migration Guide

1. Deploy new saga alongside old saga
2. Route 10% traffic to new saga
3. Monitor metrics and compare
4. Gradually increase to 100%
5. Deprecate old saga

---

## For Interviewer

**Question:**
> "У тебя тут под капотом много шагов в одном handler. Можешь разбить на отдельные файлы? Типа accept.go, swap.go?"

**Answer:**
✅ **Done!** Each saga step is now in its own file:

- `accept.go` - STEP 1 (Price quotation)
- `price.go` - STEP 2 (Position creation)
- `swap.go` - STEP 3 (Swap execution)
- `complete.go` - STEP 4 (Order completion)
- `types.go` - Shared types/interfaces
- `order_saga_refactored.go` - Main orchestrator

Each file is:
- **Focused** - Single responsibility
- **Independent** - Can be scaled/tested separately
- **Clear** - Easy to understand flow
- **Event-driven** - Following Event Sourcing best practices
