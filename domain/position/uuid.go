package position

import (
	pkguuid "market_order/pkg/uuid"
)

func generateUUID() string {
	return pkguuid.New()
}
