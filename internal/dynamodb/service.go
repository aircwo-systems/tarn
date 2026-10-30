package dynamodb

import (
	"github.com/aircwo-systems/tarn/internal/config"
)

// Service is the DynamoDB emulator; its operations live on Store.
type Service struct {
	*Store
}

func NewService(cfg *config.Config) *Service {
	return &Service{Store: NewStore(cfg)}
}
