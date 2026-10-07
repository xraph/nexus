package usage

import (
	"context"
	"errors"

	"github.com/xraph/nexus/money"
)

type service struct {
	store Store
}

// NewService creates a new usage service.
func NewService(store Store) Service {
	return &service{store: store}
}

// Record stores rec after making it consistent (see normalise). The caller's
// record is not changed.
func (s *service) Record(ctx context.Context, rec *Record) error {
	norm, err := normalise(rec)
	if err != nil {
		return err
	}
	return s.store.Insert(ctx, norm)
}

// normalise returns a copy of rec that obeys the record invariant, so a
// writer that leaves a field empty cannot store a priced $0 by accident:
//
//   - a cache hit costs exactly $0 and is PricingCached;
//   - otherwise a nil cost is PricingUnpricedModel;
//   - otherwise a cost with no status is PricingPriced;
//   - an empty outcome is OutcomeCached for a cache hit and OutcomeOK
//     otherwise.
//
// A record that says PricingUnpricedModel and also carries a cost
// contradicts itself, so it is refused rather than guessed at.
func normalise(rec *Record) (*Record, error) {
	if rec == nil {
		return nil, errors.New("usage: record is nil")
	}
	r := *rec
	switch {
	case r.Cached:
		zero := money.Zero
		r.CostUSD, r.PricingStatus = &zero, PricingCached
	case r.CostUSD == nil:
		r.PricingStatus = PricingUnpricedModel
	case r.PricingStatus == PricingUnpricedModel:
		return nil, errors.New("usage: record has a cost but its pricing status is unpriced_model")
	case r.PricingStatus == "":
		r.PricingStatus = PricingPriced
	}
	if r.Outcome == "" {
		r.Outcome = OutcomeOK
		if r.Cached {
			r.Outcome = OutcomeCached
		}
	}
	return &r, nil
}

func (s *service) MonthlySpend(ctx context.Context, tenantID string) (money.USD, error) {
	return s.store.MonthlySpend(ctx, tenantID)
}

func (s *service) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	return s.store.DailyRequests(ctx, tenantID)
}

func (s *service) Summary(ctx context.Context, tenantID, period string) (*Summary, error) {
	return s.store.Summary(ctx, tenantID, period)
}

func (s *service) Series(ctx context.Context, opts *SeriesOptions) ([]SeriesPoint, error) {
	return s.store.Series(ctx, opts)
}

func (s *service) Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error) {
	return s.store.Query(ctx, opts)
}
