# Commands Reference

This document provides a complete reference of all commands available in the system's aggregates.

---

## Order Aggregate Commands

### Core Workflow Commands

#### 1. `AcceptOrder(orderID, userID, fromAmount, fromCurrency, toCurrency, orderType)`
**Purpose**: Accept and create a new order  
**Event**: `OrderAccepted`  
**Validation**:
- fromAmount must be positive and >= 10
- orderType must be "market" or "limit"

**Usage**:
```go
order := order.NewOrder()
err := order.AcceptOrder(
    "ord_123",
    "user_456",
    1000.0,
    "USDT",
    "BTC",
    "market",
)
```

---

#### 2. `InitializeOrder()`
**Purpose**: Initialize order (load user data, prepare for execution)  
**Event**: `OrderInitialized`  
**Validation**: Status must be "pending"

**Usage**:
```go
err := order.InitializeOrder()
```

---

#### 3. `CheckBalances(availableBalance)`
**Purpose**: Verify user has sufficient funds  
**Events**: `BalanceCheckPassed` or `BalanceCheckFailed`  
**Validation**: Status must be "pending"

**Usage**:
```go
err := order.CheckBalances(1500.0) // User has 1500 USDT
// If fromAmount = 1000, generates BalanceCheckPassed
// If fromAmount = 2000, generates BalanceCheckFailed
```

---

#### 4. `QuotePrice(price, toAmount)`
**Purpose**: Set market price and calculated amount  
**Event**: `PriceQuoted`  
**Validation**: 
- Status must be "pending"
- Price and toAmount must be positive

**Usage**:
```go
price := 100000.0 // 1 BTC = 100k USDT
toAmount := 1000.0 / 100000.0 // 0.01 BTC
err := order.QuotePrice(price, toAmount)
```

---

#### 5. `SetLimitPrice(limitPrice)`
**Purpose**: Set limit price for limit orders  
**Event**: `LimitPriceSet`  
**Validation**:
- OrderType must be "limit"
- Status must be "pending"
- limitPrice must be positive

**Usage**:
```go
err := order.SetLimitPrice(98000.0) // Buy BTC at 98k USDT
```

---

#### 6. `PlaceInOrderBook(orderBookID)`
**Purpose**: Place limit order in order book for matching  
**Event**: `OrderPlacedInBook`  
**Validation**:
- OrderType must be "limit"
- Status must be "pending"

**Usage**:
```go
err := order.PlaceInOrderBook("orderbook_BTC_USDT")
```

---

#### 7. `StartSwapExecution(idempotencyKey)`
**Purpose**: Begin swap execution  
**Event**: `SwapExecuting`  
**Validation**: Status must be "pending"

**Usage**:
```go
err := order.StartSwapExecution("swap-ord_123")
```

---

#### 8. `RecordSwapExecution(txHash, fromAmount, toAmount, executedPrice, fees, slippage)`
**Purpose**: Record swap execution result  
**Event**: `SwapExecuted`  
**Validation**: Status must be "executing"

**Usage**:
```go
err := order.RecordSwapExecution(
    "0xabc123...",
    1000.0,      // From amount
    0.01,        // To amount  
    100000.0,    // Executed price
    0.5,         // Fees in USDT
    0.02,        // Slippage %
)
```

---

#### 9. `PartiallyFill(filledAmount, executedPrice, transactionHash)`
**Purpose**: Record partial fill (for limit orders)  
**Event**: `OrderPartiallyFilled`  
**Validation**:
- Status must be "executing"
- filledAmount must be > 0 and <= fromAmount

**Usage**:
```go
err := order.PartiallyFill(
    500.0,       // Filled 500 USDT of 1000 USDT order
    98000.0,     // At 98k price
    "0xdef456...",
)
```

---

#### 10. `CompleteOrder()`
**Purpose**: Mark order as completed  
**Event**: `OrderCompleted`  
**Validation**: Status must be "executing"  
**Idempotency**: Safe to call on already completed orders

**Usage**:
```go
err := order.CompleteOrder()
```

---

### Management Commands

#### 11. `UpdateOrder(params)`
**Purpose**: Update order parameters  
**Event**: `OrderUpdated`  
**Validation**: Cannot update completed/failed orders

**Usage**:
```go
err := order.UpdateOrder(map[string]interface{}{
    "from_amount": 1500.0,
    "to_amount": 0.015,
})
```

---

#### 12. `CancelOrder(reason)`
**Purpose**: Cancel order (user-initiated)  
**Event**: `OrderCancelled`  
**Validation**:
- Cannot cancel completed orders
- Cannot cancel executing orders
**Idempotency**: Safe to call on already cancelled orders

**Usage**:
```go
err := order.CancelOrder("user_requested")
```

---

#### 13. `FailOrder(reason)`
**Purpose**: Fail order (system-initiated, compensation)  
**Event**: `OrderFailed`  
**Validation**: Cannot fail completed orders  
**Idempotency**: Safe to call on already failed orders

**Usage**:
```go
err := order.FailOrder("insufficient_liquidity")
```

---

## Position Aggregate Commands

### 1. `CreatePosition(positionID, userID)`
**Purpose**: Create new position  
**Event**: `PositionCreated`

**Usage**:
```go
position := position.NewPosition()
err := position.CreatePosition("pos_789", "user_456")
```

---

### 2. `AddOrder(orderID, toAmount, totalValue, pnl)`
**Purpose**: Add order to position  
**Event**: `PositionUpdated`  
**Validation**: Status must be "open"

**Usage**:
```go
err := position.AddOrder(
    "ord_123",
    0.01,        // toAmount in BTC
    1000.0,      // totalValue in USD
    0.0,         // PnL (0 for first order)
)
```

---

### 3. `ClosePosition(reason)`
**Purpose**: Close position (compensation)  
**Event**: `PositionClosed`  
**Idempotency**: Safe to call on already closed positions

**Usage**:
```go
err := position.ClosePosition("order_failed")
```

---

## OrderBook Aggregate Commands

### 1. `CreateOrderBook(orderBookID, tradingPair)`
**Purpose**: Create order book for trading pair  
**Event**: `OrderBookCreated`

**Usage**:
```go
orderBook := orderbook.NewOrderBook()
err := orderBook.CreateOrderBook("ob_001", "BTC/USDT")
```

---

### 2. `AddLimitOrder(orderID, userID, price, amount, side)`
**Purpose**: Add limit order to book  
**Event**: `LimitOrderAdded`  
**Validation**:
- Status must be "active"
- side must be "buy" or "sell"
- price and amount must be positive

**Auto-sorting**:
- Buy orders: sorted by price DESC (highest first)
- Sell orders: sorted by price ASC (lowest first)

**Usage**:
```go
err := orderBook.AddLimitOrder(
    "ord_123",
    "user_456",
    98000.0,     // Price: buy BTC at 98k USDT
    0.01,        // Amount: 0.01 BTC
    "buy",
)
```

---

### 3. `MatchOrders()`
**Purpose**: Execute matching algorithm  
**Event**: `OrdersMatched` (if match found)  
**Validation**: Status must be "active"

**Algorithm**:
```
if len(buyOrders) > 0 && len(sellOrders) > 0:
    bestBuy = buyOrders[0]   // Highest buy price
    bestSell = sellOrders[0] // Lowest sell price
    
    if bestBuy.Price >= bestSell.Price:
        matchedAmount = min(bestBuy.Remaining, bestSell.Remaining)
        matchedPrice = (bestBuy.Price + bestSell.Price) / 2
        
        Generate OrdersMatched event
        Update/remove matched orders
```

**Usage**:
```go
err := orderBook.MatchOrders()
// If no match: returns nil, no event
// If match: generates OrdersMatched event
```

---

### 4. `CancelLimitOrder(orderID, side)`
**Purpose**: Remove order from book  
**Event**: `LimitOrderCancelled`  
**Validation**:
- Status must be "active"
- Order must exist in book

**Usage**:
```go
err := orderBook.CancelLimitOrder("ord_123", "buy")
```

---

### 5. `UpdatePrice(newPrice, source)`
**Purpose**: Update price from WebSocket feed  
**Event**: `PriceUpdated`  
**Validation**: newPrice must be positive

**Usage**:
```go
err := orderBook.UpdatePrice(
    99500.0,     // New price
    "binance",   // Source: binance, coinbase, uniswap, etc.
)
```

---

## Command Patterns

### Idempotency Pattern
Many commands are idempotent (safe to call multiple times):

```go
// Idempotent: check current state first
func (o *Order) CompleteOrder() error {
    if o.Status == OrderStatusCompleted {
        return nil  // Already completed, skip
    }
    
    if o.Status != OrderStatusExecuting {
        return fmt.Errorf("cannot complete: status is %s", o.Status)
    }
    
    // Generate event...
}
```

### State Machine Pattern
Commands enforce state transitions:

```
pending → executing → completed
pending → failed
executing → failed
```

Invalid transitions return errors:
```go
if o.Status == OrderStatusCompleted {
    return errors.New("cannot modify completed order")
}
```

### Event Sourcing Pattern
All commands generate events:

```go
func (o *Order) AcceptOrder(...) error {
    // 1. Validate business rules
    if fromAmount <= 0 {
        return errors.New("amount must be positive")
    }
    
    // 2. Generate event
    event := OrderAccepted{...}
    
    // 3. Apply event (update state + add to Changes)
    return o.Apply(event)
}
```

---

## Workflow Examples

### Market Order Workflow
```go
// 1. Accept order
order.AcceptOrder("ord_123", "user_456", 1000, "USDT", "BTC", "market")

// 2. Initialize
order.InitializeOrder()

// 3. Check balance
order.CheckBalances(1500.0) // User has enough

// 4. Get price
order.QuotePrice(100000.0, 0.01)

// 5. Execute
order.StartSwapExecution("swap-ord_123")
order.RecordSwapExecution("0xabc...", 1000, 0.01, 100000, 0.5, 0.02)

// 6. Complete
order.CompleteOrder()
```

### Limit Order Workflow
```go
// 1. Accept order
order.AcceptOrder("ord_123", "user_456", 1000, "USDT", "BTC", "limit")

// 2. Initialize
order.InitializeOrder()

// 3. Check balance
order.CheckBalances(1500.0)

// 4. Set limit price
order.SetLimitPrice(98000.0)

// 5. Place in order book
order.PlaceInOrderBook("ob_BTC_USDT")
orderBook.AddLimitOrder("ord_123", "user_456", 98000, 0.01020408, "buy")

// 6. Wait for match...
// When price drops to 98k:
orderBook.MatchOrders()
// → OrdersMatched event published

// 7. Order receives match notification
order.PartiallyFill(500, 98000, "0xdef...")
// or
order.CompleteOrder()
```

---

## Event-to-Command Mapping

| Event | Triggers Command |
|-------|-----------------|
| OrderAccepted | Saga → order.InitializeOrder() |
| OrderInitialized | Saga → order.CheckBalances() |
| BalanceCheckPassed | Saga → order.QuotePrice() (market) or order.SetLimitPrice() (limit) |
| PriceQuoted | Saga → order.StartSwapExecution() |
| LimitPriceSet | Saga → order.PlaceInOrderBook() |
| OrderPlacedInBook | Saga → orderBook.AddLimitOrder() |
| PriceUpdated | MatchingEngine → orderBook.MatchOrders() |
| OrdersMatched | Saga → order.PartiallyFill() or order.CompleteOrder() |
| SwapExecuted | Saga → order.CompleteOrder() |
| OrderCompleted | Saga → position.AddOrder() |
| OrderFailed | Saga → position.ClosePosition() |

---

This reference provides all commands needed to implement the complete order execution workflow for both market and limit orders.
