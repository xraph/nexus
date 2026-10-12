package storetest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/usage"
)

// Record builds a usage record with every field filled. An empty cost
// builds an unpriced record (nil cost, unpriced_model).
func Record(tenantID id.TenantID, cost string) *usage.Record {
	r := &usage.Record{
		ID:               id.NewUsageID(),
		TenantID:         tenantID,
		KeyID:            id.NewKeyID(),
		RequestID:        id.NewRequestID(),
		Provider:         "openai",
		Model:            "gpt-4o",
		PromptTokens:     120,
		CompletionTokens: 30,
		TotalTokens:      150,
		PricingStatus:    usage.PricingPriced,
		Outcome:          usage.OutcomeOK,
		Latency:          1500 * time.Millisecond,
		StatusCode:       200,
		CreatedAt:        Now(),
	}
	if cost == "" {
		r.PricingStatus = usage.PricingUnpricedModel
	} else {
		c := money.MustParse(cost)
		r.CostUSD = &c
	}
	return r
}

// InsertRecord stores r.
func InsertRecord(t *testing.T, s store.Store, r *usage.Record) {
	t.Helper()
	if err := s.Usage().Insert(context.Background(), r); err != nil {
		t.Fatalf("insert usage: %v", err)
	}
}

// SameRecord fails the test unless got and want match field by field. Cost
// is compared as an amount, so 0.150 and 0.15 are the same.
func SameRecord(t *testing.T, got, want *usage.Record) {
	t.Helper()
	if got == nil {
		t.Fatalf("record is nil")
	}
	g, w := *got, *want
	for _, p := range [][2]id.ID{{g.ID, w.ID}, {g.TenantID, w.TenantID}, {g.KeyID, w.KeyID}, {g.RequestID, w.RequestID}} {
		if p[0].String() != p[1].String() {
			t.Errorf("record id = %q, want %q", p[0], p[1])
		}
	}
	switch {
	case g.CostUSD == nil && w.CostUSD == nil:
	case g.CostUSD == nil || w.CostUSD == nil || !g.CostUSD.Equal(*w.CostUSD):
		t.Errorf("cost = %v, want %v", g.CostUSD, w.CostUSD)
	}
	if !g.CreatedAt.Equal(w.CreatedAt) {
		t.Errorf("created_at = %s, want %s", g.CreatedAt, w.CreatedAt)
	}
	g.ID, w.ID, g.TenantID, w.TenantID, g.KeyID, w.KeyID, g.RequestID, w.RequestID = id.Nil, id.Nil, id.Nil, id.Nil, id.Nil, id.Nil, id.Nil, id.Nil
	g.CostUSD, w.CostUSD = nil, nil
	g.CreatedAt, w.CreatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("record round trip:\n got %+v\nwant %+v", g, w)
	}
}

// FindRecord returns the record with the given id from an unfiltered query.
func FindRecord(t *testing.T, s store.Store, rid id.UsageID) *usage.Record {
	t.Helper()
	cursor := ""
	for {
		res, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 500, Cursor: cursor})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		for _, r := range res.Items {
			if r.ID.String() == rid.String() {
				return r
			}
		}
		if res.NextCursor == "" {
			t.Fatalf("record %s not found", rid)
		}
		cursor = res.NextCursor
	}
}
