package storetest

import (
	"context"
	"errors"
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
	// A record stored without metadata reads back with an empty map.
	if len(g.Metadata) == 0 && len(w.Metadata) == 0 {
		g.Metadata, w.Metadata = nil, nil
	}
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

// metaRecord is a priced record at when, labelled with the given pairs
// (key, value, key, value...).
func metaRecord(tenantID id.TenantID, cost string, when time.Time, pairs ...string) *usage.Record {
	r := Record(tenantID, cost)
	r.CreatedAt = when
	r.Metadata = map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.Metadata[pairs[i]] = pairs[i+1]
	}
	return r
}

// UsageMetadataRoundTrips checks that metadata comes back equal, and that a
// record stored without any reads back as an empty, non-nil map.
func UsageMetadataRoundTrips(t *testing.T, s store.Store) {
	t.Helper()
	tn := InsertTenant(t, s)
	with := Record(tn.ID, "0.01")
	with.Metadata = map[string]string{"project": "p1"}
	InsertRecord(t, s, with)
	without := Record(tn.ID, "0.01")
	InsertRecord(t, s, without)

	if got := FindRecord(t, s, with.ID); !reflect.DeepEqual(got.Metadata, map[string]string{"project": "p1"}) {
		t.Errorf("metadata = %#v, want project=p1", got.Metadata)
	}
	if got := FindRecord(t, s, without.ID); got.Metadata == nil || len(got.Metadata) != 0 {
		t.Errorf("metadata of a record stored without any = %#v, want an empty, non-nil map", got.Metadata)
	}
}

// UsageQueryFiltersByMetadata checks the filter is exact, needs every pair,
// and pages without losing or repeating a record.
func UsageQueryFiltersByMetadata(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	tn := InsertTenant(t, s)
	now := Now()
	for _, r := range []*usage.Record{
		metaRecord(tn.ID, "0.01", now, "project", "p1", "run", "r1"),
		metaRecord(tn.ID, "0.01", now, "project", "p1", "run", "r2"),
		metaRecord(tn.ID, "0.01", now, "project", "p2", "run", "r1"),
	} {
		InsertRecord(t, s, r)
	}
	count := func(m map[string]string) int {
		t.Helper()
		res, err := s.Usage().Query(ctx, &usage.QueryOptions{TenantID: tn.ID.String(), Metadata: m})
		if err != nil {
			t.Fatalf("query %v: %v", m, err)
		}
		return len(res.Items)
	}
	for _, c := range []struct {
		name string
		m    map[string]string
		want int
	}{
		{"one pair", map[string]string{"project": "p1"}, 2},
		{"two pairs", map[string]string{"project": "p1", "run": "r2"}, 1},
		{"no match", map[string]string{"project": "p3"}, 0},
		{"value is exact", map[string]string{"project": "p"}, 0},
		{"empty filters nothing", map[string]string{}, 3},
		{"nil filters nothing", nil, 3},
	} {
		if got := count(c.m); got != c.want {
			t.Errorf("%s: %d records, want %d", c.name, got, c.want)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		res, err := s.Usage().Query(ctx, &usage.QueryOptions{
			TenantID: tn.ID.String(), Metadata: map[string]string{"project": "p1"}, Limit: 1, Cursor: cursor,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range res.Items {
			if seen[r.ID.String()] {
				t.Errorf("record %s came back twice", r.ID)
			}
			seen[r.ID.String()] = true
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(seen) != 2 {
		t.Errorf("paging with Limit 1 saw %d records, want 2", len(seen))
	}
}

// UsageSeriesFiltersByMetadata checks a series sums only the matching records.
func UsageSeriesFiltersByMetadata(t *testing.T, s store.Store) {
	t.Helper()
	tn := InsertTenant(t, s)
	start := usage.BucketStart(time.Now().Add(-3*time.Hour), usage.BucketHour)
	for _, r := range []*usage.Record{
		metaRecord(tn.ID, "0.10", start.Add(5*time.Minute), "project", "p1"),
		metaRecord(tn.ID, "0.20", start.Add(20*time.Minute), "project", "p1"),
		metaRecord(tn.ID, "0.40", start.Add(30*time.Minute), "project", "p2"),
	} {
		InsertRecord(t, s, r)
	}
	got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
		TenantID: tn.ID.String(), Start: start, End: start.Add(time.Hour), Bucket: usage.BucketHour,
		Metadata: map[string]string{"project": "p1"},
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("series = %v, %v", got, err)
	}
	if got[0].Requests != 2 || got[0].CostUSD.String() != "0.3" {
		t.Errorf("p1 hour = %+v, want 2 requests costing 0.3", got[0])
	}
}

// UsageMetadataFilterRejectsBadKeys checks a filter is validated like record
// metadata, on both reads.
func UsageMetadataFilterRejectsBadKeys(t *testing.T, s store.Store) {
	t.Helper()
	bad := map[string]string{"a.b": "x"}
	if _, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Metadata: bad}); !errors.Is(err, usage.ErrInvalidMetadata) {
		t.Errorf("Query err = %v, want ErrInvalidMetadata", err)
	}
	start := usage.BucketStart(time.Now(), usage.BucketHour)
	_, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{Start: start, End: start.Add(time.Hour), Bucket: usage.BucketHour, Metadata: bad})
	if !errors.Is(err, usage.ErrInvalidMetadata) {
		t.Errorf("Series err = %v, want ErrInvalidMetadata", err)
	}
}
