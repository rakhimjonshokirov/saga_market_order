# Документация по Event Flow в Saga

## Диаграмма потока событий

```
┌─────────────────────────────────────────────────────────────────────────┐
│ ЗАПРОС ПОЛЬЗОВАТЕЛЯ: Создать рыночный ордер                             │
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
                   ├─► [ПРОВЕРКА ИДЕМПОТЕНТНОСТИ]
                   │   ✓ Проверка, обработан ли EventID
                   │   ✓ Пропуск, если уже обработан
                   │
                   ├─► [ШАГ 1: Получение цены]
                   │   CMD: GetMarketPrice()
                   │   EVENT: PriceQuoted
                   │   AGGREGATE: Order (v2)
                   │
                   ├─► [ШАГ 2: Создание позиции]
                   │   CMD: CreatePosition()
                   │   EVENT: PositionCreated
                   │   AGGREGATE: Position (v1)
                   │
                   ├─► [ШАГ 3: Исполнение swap]
                   │   CMD: ExecuteSwap()
                   │   EVENTS: SwapExecuting → SwapExecuted
                   │   AGGREGATE: Order (v3, v4)
                   │
                   └─► [ШАГ 4: Завершение ордера и обновление позиции]
                       USECASE: CompleteOrderAndUpdatePositionUseCase
                       EVENTS: OrderCompleted + PositionUpdated
                       AGGREGATES: Order (v5) + Position (v2)
                       ✓ АТОМАРНАЯ ТРАНЗАКЦИЯ (оба или ничего)
                       ✓ Отметка EventID как обработанного
```

---

## Подробный разбор событий

### 1. Событие OrderAccepted
**Название события:** `OrderAccepted`

**Слушатель:** `handleOrderAccepted` ([order_saga.go:92](application/saga/order_saga.go#L92))

**Что делает слушатель:**
- Проверяет идемпотентность через `ProcessedEventsRepository`
- Оркеструет 4-шаговый Saga workflow
- Вызывает внешние сервисы (PriceService, TradeWorker)
- Выполняет use case (CompleteOrderAndUpdatePositionUseCase)

**Обновляемые агрегаты:**
- **Order Aggregate** (обновляется несколько раз в течение саги)
- **Position Aggregate** (создается и обновляется)

**Гарантия идемпотентности:**
```go
// Проверка, обработано ли событие
processed, err := s.processedEvents.IsProcessed(ctx, evt.EventID)
if processed {
    return nil // Пропускаем дубликат
}

// ... выполнение саги ...

// Отметка как обработанного в конце
s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga")
```

**Результирующие события:**
- `PriceQuoted`
- `PositionCreated`
- `SwapExecuting`
- `SwapExecuted`
- `OrderCompleted`
- `PositionUpdated`

---

### 2. Событие PriceQuoted
**Название события:** `PriceQuoted` ([order/events.go:62](domain/order/events.go#L62))

**Вызывается методом:** `Order.QuotePrice()` ([order_saga.go:132](application/saga/order_saga.go#L132))

**Что происходит:**
- Агрегат Order записывает рыночную цену и рассчитанный toAmount
- Состояние Order: `pending` → `pending` (статус не меняется, но обогащается данными о цене)

**Обновляемый агрегат:** Order (версия увеличивается: v1 → v2)

**Идемпотентность:**
Event sourcing гарантирует контроль версий. Дублирующиеся события отклоняются оптимистичной блокировкой.

---

### 3. Событие PositionCreated
**Название события:** `PositionCreated` ([position/events.go:29](domain/position/events.go#L29))

**Вызывается методом:** `Position.CreatePosition()` ([order_saga.go:146](application/saga/order_saga.go#L146))

**Что происходит:**
- Создается новый агрегат Position для пользователя
- Начальное состояние: `status=open`, `remainingAmount=0`

**Обновляемый агрегат:** Position (версия: v1)

**Идемпотентность:**
Position создается с уникальным UUID. При повторе саги проверка идемпотентности предотвращает дублирование.

---

### 4. События SwapExecuting → SwapExecuted
**Названия событий:** `SwapExecuting`, `SwapExecuted` ([order/events.go:74-92](domain/order/events.go#L74))

**Вызываются:**
1. `Order.StartSwapExecution()` → `SwapExecuting` ([order_saga.go:163](application/saga/order_saga.go#L163))
2. `Order.RecordSwapExecution()` → `SwapExecuted` ([order_saga.go:186](application/saga/order_saga.go#L186))

**Что происходит:**
- CMD: `TradeWorker.ExecuteSwap()` с ключом идемпотентности
- Order записывает хеш swap-транзакции, исполненную цену, комиссии, проскальзывание
- Внешний swap исполняется на блокчейне/DEX

**Обновляемый агрегат:** Order (v3 → v4)

**Идемпотентность:**
```go
idempotencyKey := fmt.Sprintf("swap-%s", orderID)
swapReq := SwapRequest{IdempotencyKey: idempotencyKey, ...}
```
TradeWorker должен реализовать идемпотентное исполнение swap с этим ключом.

---

### 5. OrderCompleted + PositionUpdated (АТОМАРНО)
**Названия событий:** `OrderCompleted`, `PositionUpdated`

**Вызывается:** `CompleteOrderAndUpdatePositionUseCase.Execute()` ([order_saga.go:199](application/saga/order_saga.go#L199))

**Что делает UseCase:**
1. Загружает агрегат Order → вызывает `CompleteOrder()` → генерирует событие `OrderCompleted`
2. Загружает агрегат Position → вызывает `AddOrder()` → генерирует событие `PositionUpdated`
3. **Сохраняет оба события в ОДНОЙ транзакции** ([complete_order_and_update_position.go:78](application/usecases/complete_order_and_update_position.go#L78))

```go
allEvents := append(o.Changes, p.Changes...)
eventStore.Save(ctx, allEvents) // АТОМАРНО
```

**Обновляемые агрегаты:**
- **Order:** v4 → v5 (status: `executing` → `completed`)
- **Position:** v1 → v2 (добавляется ордер в позицию, обновляется remainingAmount, PnL)

**Гарантия идемпотентности:**
- Event Store гарантирует атомарность: оба агрегата обновляются или ни один
- Если сага повторяется после swap, но до завершения, проверка идемпотентности предотвращает повторное исполнение
- Event ID отмечается как обработанный ПОСЛЕ успешного атомарного сохранения

---

## Изменения состояния агрегатов

### Агрегат Order
```
v1: OrderAccepted      (status: pending)
v2: PriceQuoted        (добавлена цена)
v3: SwapExecuting      (status: executing)
v4: SwapExecuted       (записаны детали swap)
v5: OrderCompleted     (status: completed)
```

### Агрегат Position
```
v1: PositionCreated    (status: open, remainingAmount: 0)
v2: PositionUpdated    (добавлен ордер, remainingAmount: 0.01btc)
```

---

## Сводная таблица

| Событие           | Обработчик/CMD                     | Обновляемые агрегаты | Метод идемпотентности        |
|-------------------|------------------------------------|----------------------|------------------------------|
| OrderAccepted     | handleOrderAccepted (saga)         | Order, Position      | Проверка ProcessedEvents     |
| PriceQuoted       | Order.QuotePrice()                 | Order (v2)           | Версионирование event sourcing |
| PositionCreated   | Position.CreatePosition()          | Position (v1)        | Версионирование event sourcing |
| SwapExecuting     | Order.StartSwapExecution()         | Order (v3)           | Версионирование event sourcing |
| SwapExecuted      | Order.RecordSwapExecution()        | Order (v4)           | Ключ идемпотентности swap    |
| OrderCompleted    | CompleteOrderAndUpdatePositionUC   | Order (v5)           | Транзакция Event Store       |
| PositionUpdated   | CompleteOrderAndUpdatePositionUC   | Position (v2)        | Транзакция Event Store       |
