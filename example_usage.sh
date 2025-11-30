#!/bin/bash

# Example: Create a market order

echo "Creating market order: 1000 USDT -> BTC..."
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-123",
    "from_amount": 1000,
    "from_currency": "USDT",
    "to_currency": "BTC",
    "order_type": "market"
  }'

echo -e "\n\n"

echo "Creating market order: 4000 USDT -> ETH..."
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-456",
    "from_amount": 4000,
    "from_currency": "USDT",
    "to_currency": "ETH",
    "order_type": "market"
  }'

echo -e "\n\n"

echo "Checking health..."
curl http://localhost:8080/health

echo -e "\n"
