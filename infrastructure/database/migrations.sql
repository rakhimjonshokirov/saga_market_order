-- =====================================================
-- Event Sourcing Database Schema
-- =====================================================

-- =====================================================
-- 1. Events Table (Event Store)
-- =====================================================
CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,              -- Уникальный ID события
    aggregate_id UUID NOT NULL,                 -- ID агрегата (orderID, positionID)
    aggregate_type VARCHAR(50) NOT NULL,        -- Тип агрегата: "Order", "Position"
    event_type VARCHAR(100) NOT NULL,           -- Тип события: "OrderAccepted", "SwapExecuted"
    event_data JSONB NOT NULL,                  -- Полные данные события в JSON
    metadata JSONB,                             -- Метаданные (user_agent, trace_id, etc.)
    version INT NOT NULL,                       -- Версия агрегата (optimistic locking)
    created_at TIMESTAMP DEFAULT NOW()
);

-- IMPORTANT: Уникальность (aggregate_id, version) для Optimistic Locking
CREATE UNIQUE INDEX IF NOT EXISTS idx_aggregate_version
    ON events(aggregate_id, version);

-- Индекс для быстрой загрузки событий агрегата
CREATE INDEX IF NOT EXISTS idx_events_aggregate_id
    ON events(aggregate_id);

-- Индекс для поиска по типу события
CREATE INDEX IF NOT EXISTS idx_events_type
    ON events(event_type);

-- Индекс для временной сортировки
CREATE INDEX IF NOT EXISTS idx_events_created_at
    ON events(created_at DESC);

COMMENT ON TABLE events IS 'Event Store: хранит все доменные события';
COMMENT ON COLUMN events.version IS 'Версия агрегата - защита от race conditions (optimistic locking)';
COMMENT ON INDEX idx_aggregate_version IS 'UNIQUE constraint для защиты от дублирования версий';


-- =====================================================
-- 2. Outbox Table (Transactional Outbox Pattern)
-- =====================================================
CREATE TABLE IF NOT EXISTS outbox (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,              -- Ссылка на events.event_id
    aggregate_id UUID NOT NULL,                 -- ID агрегата
    event_type VARCHAR(100) NOT NULL,           -- Тип события для routing в RabbitMQ
    event_data JSONB NOT NULL,                  -- Данные для публикации
    published BOOLEAN DEFAULT FALSE,            -- Флаг: опубликовано ли событие
    published_at TIMESTAMP,                     -- Когда опубликовано
    created_at TIMESTAMP DEFAULT NOW()
);

-- Индекс для выборки непубликованных событий
CREATE INDEX IF NOT EXISTS idx_outbox_published
    ON outbox(published, created_at)
    WHERE published = FALSE;

-- Индекс для поиска по event_id
CREATE INDEX IF NOT EXISTS idx_outbox_event_id
    ON outbox(event_id);

COMMENT ON TABLE outbox IS 'Transactional Outbox: гарантирует публикацию событий в RabbitMQ';
COMMENT ON COLUMN outbox.published IS 'FALSE = событие ждёт публикации, TRUE = опубликовано';


-- =====================================================
-- 3. Processed Events Table (Idempotency)
-- =====================================================
CREATE TABLE IF NOT EXISTS processed_events (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,              -- ID обработанного события
    aggregate_id UUID NOT NULL,                 -- ID агрегата для логирования
    event_type VARCHAR(100) NOT NULL,           -- Тип события
    processed_at TIMESTAMP DEFAULT NOW(),       -- Когда обработано
    processed_by VARCHAR(100)                   -- Какой сервис обработал (saga, notification-service)
);

-- Индекс для проверки идемпотентности (быстрый lookup)
CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_events_event_id
    ON processed_events(event_id);

-- Индекс для аудита по агрегату
CREATE INDEX IF NOT EXISTS idx_processed_events_aggregate
    ON processed_events(aggregate_id, event_type);

COMMENT ON TABLE processed_events IS 'Таблица для идемпотентности: предотвращает дублирование обработки событий';
COMMENT ON COLUMN processed_events.event_id IS 'Уникальный ID события - проверяется перед обработкой';


-- =====================================================
-- 4. Saga State Table (Optional: для persistent saga state)
-- =====================================================
CREATE TABLE IF NOT EXISTS saga_state (
    id BIGSERIAL PRIMARY KEY,
    saga_id UUID NOT NULL UNIQUE,               -- Уникальный ID саги
    saga_type VARCHAR(50) NOT NULL,             -- Тип саги: "OrderExecutionSaga"
    aggregate_id UUID NOT NULL,                 -- ID основного агрегата (orderID)
    current_step VARCHAR(100) NOT NULL,         -- Текущий шаг: "PriceQuoted", "SwapExecuting"
    status VARCHAR(50) NOT NULL,                -- Статус: "running", "completed", "failed", "compensating"
    payload JSONB,                              -- Данные саги (orderID, positionID, etc.)
    started_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP
);

-- Индекс для поиска саги по агрегату
CREATE INDEX IF NOT EXISTS idx_saga_aggregate
    ON saga_state(aggregate_id);

-- Индекс для поиска активных саг
CREATE INDEX IF NOT EXISTS idx_saga_status
    ON saga_state(status, updated_at)
    WHERE status IN ('running', 'compensating');

COMMENT ON TABLE saga_state IS 'Персистентное состояние саг (optional: для recovery после рестарта)';


-- =====================================================
-- 5. Read Model Tables (CQRS - для queries)
-- =====================================================

-- Order Read Model (проекция для чтения)
CREATE TABLE IF NOT EXISTS order_view (
    order_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    from_amount DECIMAL(20, 8) NOT NULL,
    from_currency VARCHAR(10) NOT NULL,
    to_currency VARCHAR(10) NOT NULL,
    to_amount DECIMAL(20, 8),
    executed_price DECIMAL(20, 8),
    order_type VARCHAR(20) NOT NULL,            -- "market", "limit"
    status VARCHAR(20) NOT NULL,                -- "pending", "executing", "completed", "failed"
    transaction_hash VARCHAR(100),
    version INT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_view_user
    ON order_view(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_order_view_status
    ON order_view(status, created_at DESC);

COMMENT ON TABLE order_view IS 'Read Model для Order - обновляется из событий через проектор';


-- Position Read Model (проекция для чтения)
CREATE TABLE IF NOT EXISTS position_view (
    position_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    remaining_amount DECIMAL(20, 8) NOT NULL,
    total_value DECIMAL(20, 8),
    pnl DECIMAL(20, 8),
    status VARCHAR(20) NOT NULL,                -- "open", "closed"
    version INT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_position_view_user
    ON position_view(user_id);

COMMENT ON TABLE position_view IS 'Read Model для Position - обновляется из событий';


-- Position Orders (many-to-many)
CREATE TABLE IF NOT EXISTS position_orders (
    position_id UUID NOT NULL,
    order_id UUID NOT NULL,
    added_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (position_id, order_id)
);

CREATE INDEX IF NOT EXISTS idx_position_orders_position
    ON position_orders(position_id);


-- =====================================================
-- 6. Notification Log (Audit trail для уведомлений)
-- =====================================================
CREATE TABLE IF NOT EXISTS notification_log (
    id BIGSERIAL PRIMARY KEY,
    notification_id UUID NOT NULL UNIQUE,       -- Hash(orderId + eventType)
    order_id UUID NOT NULL,
    user_id UUID NOT NULL,
    channel VARCHAR(50) NOT NULL,               -- "telegram", "email", "webhook"
    message TEXT NOT NULL,
    sent BOOLEAN DEFAULT FALSE,
    sent_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_log_sent
    ON notification_log(sent, created_at)
    WHERE sent = FALSE;

COMMENT ON TABLE notification_log IS 'Лог отправленных уведомлений (для идемпотентности и аудита)';


-- =====================================================
-- Example Data
-- =====================================================

-- Example: Order lifecycle events for order "ord_123"
/*
INSERT INTO events (event_id, aggregate_id, aggregate_type, event_type, event_data, version) VALUES
('evt_001', 'ord_123', 'Order', 'OrderAccepted',
 '{"user_id": "user_456", "from_amount": 1000, "from_currency": "USDT", "to_currency": "BTC", "order_type": "market"}', 1),

('evt_002', 'ord_123', 'Order', 'PriceQuoted',
 '{"price": 100000, "to_amount": 0.01}', 2),

('evt_003', 'ord_123', 'Order', 'SwapExecuting',
 '{"idempotency_key": "swap-ord_123"}', 3),

('evt_004', 'ord_123', 'Order', 'SwapExecuted',
 '{"transaction_hash": "0xabc...", "to_amount": 0.01, "executed_price": 100000}', 4),

('evt_005', 'ord_123', 'Order', 'OrderCompleted',
 '{"from_amount": 1000, "to_amount": 0.01, "executed_price": 100000}', 5);
*/


-- =====================================================
-- Queries Examples
-- =====================================================

-- Load all events for an aggregate (replay)
-- SELECT * FROM events WHERE aggregate_id = 'ord_123' ORDER BY version ASC;

-- Check if event already processed (idempotency)
-- SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = 'evt_001');

-- Get unpublished events (outbox worker)
-- SELECT * FROM outbox WHERE published = FALSE ORDER BY created_at ASC LIMIT 100;

-- Get user's orders (read model)
-- SELECT * FROM order_view WHERE user_id = 'user_456' ORDER BY created_at DESC;

-- Get active sagas (recovery)
-- SELECT * FROM saga_state WHERE status = 'running' AND updated_at < NOW() - INTERVAL '5 minutes';
