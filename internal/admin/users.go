package admin

import (
	"context"
	"cpa-usage-ranking/internal/clients"
)

func (s *Service) Users(ctx context.Context, query clients.ListQuery) (clients.Page, error) {
	return s.clients.List(ctx, query)
}
