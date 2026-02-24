package usage

import "context"

type service struct {
	store Store
}

// NewService creates a new usage service.
func NewService(store Store) Service {
	return &service{store: store}
}

func (s *service) Record(ctx context.Context, rec *Record) error {
	return s.store.Insert(ctx, rec)
}

func (s *service) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	return s.store.MonthlySpend(ctx, tenantID)
}

func (s *service) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	return s.store.DailyRequests(ctx, tenantID)
}

func (s *service) Summary(ctx context.Context, tenantID, period string) (*Summary, error) {
	return s.store.Summary(ctx, tenantID, period)
}

func (s *service) Query(ctx context.Context, opts *QueryOptions) ([]*Record, int, error) {
	return s.store.Query(ctx, opts)
}
