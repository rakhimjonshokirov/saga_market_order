-- Drop projection tables (not needed - EventStore is source of truth)

DROP TABLE IF EXISTS order_view CASCADE;
DROP TABLE IF EXISTS position_view CASCADE;
DROP TABLE IF EXISTS position_orders CASCADE;

COMMENT ON DATABASE eventstore IS 'EventStore is the ONLY source of truth. No projections needed.';
