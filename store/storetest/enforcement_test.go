package storetest_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestDailyRequestsLeaveOutRefusals(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		ok := storetest.Record(tn.ID, "0.01")
		blocked := storetest.Record(tn.ID, "")
		blocked.Outcome = usage.OutcomeBlocked
		refused := storetest.Record(tn.ID, "")
		refused.Outcome, refused.PricingStatus, refused.RefusalCode, refused.StatusCode = usage.OutcomeRefused, usage.PricingNotCharged, "rate_limited", 429
		zero := money.Zero
		refused.CostUSD = &zero
		for _, r := range []*usage.Record{ok, blocked, refused} {
			storetest.InsertRecord(t, s, r)
		}
		n, err := s.Usage().DailyRequests(ctx, tn.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("daily requests = %d, want 2: a refused request must not use up the quota that refused it", n)
		}
		all, err := s.Usage().DailyRequests(ctx, "")
		if err != nil || all != 2 {
			t.Fatalf("every tenant = %d, %v; want 2", all, err)
		}
		other, err := s.Usage().DailyRequests(ctx, id.NewTenantID().String())
		if err != nil || other != 0 {
			t.Fatalf("another tenant = %d, %v; want 0", other, err)
		}
	})
}
