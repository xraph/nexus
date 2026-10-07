package storetest_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

func TestUsagePagesNeitherSkipNorRepeat(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		want := make([]string, 0, 7)
		for range 7 {
			r := storetest.Record(tn.ID, "0.01")
			storetest.InsertRecord(t, s, r)
			want = append(want, r.ID.String())
		}
		slices.Sort(want)
		slices.Reverse(want) // newest first by id

		var got []string
		cursor := ""
		for {
			res, err := s.Usage().Query(ctx, &usage.QueryOptions{TenantID: tn.ID.String(), Limit: 3, Cursor: cursor})
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if len(res.Items) > 3 {
				t.Fatalf("page of %d over a limit of 3", len(res.Items))
			}
			for _, r := range res.Items {
				got = append(got, r.ID.String())
			}
			if res.NextCursor == "" {
				break
			}
			cursor = res.NextCursor
		}
		if !slices.Equal(got, want) {
			t.Fatalf("paged ids:\n got %v\nwant %v", got, want)
		}
	})
}

func TestUsageFiltersRunAtTheStore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		a, b := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		match := storetest.Record(a.ID, "0.01")
		match.Provider, match.Model, match.Outcome, match.RefusalCode = "anthropic", "claude-sonnet-4-5", usage.OutcomeRefused, "rate_limited"
		match.CreatedAt = storetest.Now().Add(-time.Hour)
		decoys := []*usage.Record{
			storetest.Record(b.ID, "0.01"), // other tenant
			storetest.Record(a.ID, "0.01"), // other provider and outcome
			func() *usage.Record { r := *match; r.ID, r.KeyID = id.NewUsageID(), id.NewKeyID(); return &r }(), // other key
			func() *usage.Record {
				r := *match
				r.ID = id.NewUsageID()
				r.CreatedAt = storetest.Now().Add(-48 * time.Hour)
				return &r
			}(), // too old
		}
		for _, r := range append([]*usage.Record{match}, decoys...) {
			storetest.InsertRecord(t, s, r)
		}
		res, err := s.Usage().Query(ctx, &usage.QueryOptions{
			TenantID: a.ID.String(), KeyID: match.KeyID.String(), Provider: "anthropic",
			Model: "claude-sonnet-4-5", Outcome: usage.OutcomeRefused,
			StartTime: storetest.Now().Add(-2 * time.Hour), EndTime: storetest.Now(),
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(res.Items) != 1 || res.Items[0].ID.String() != match.ID.String() {
			ids := make([]string, len(res.Items))
			for i, r := range res.Items {
				ids[i] = r.ID.String()
			}
			t.Fatalf("filtered = %v, want only %s", ids, match.ID)
		}
	})
}

func TestTenantListPagesAndSearches(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		for _, slug := range []string{"acme-search", "acme_billing", "globex", "100%-off"} {
			tn := storetest.Tenant(slug)
			if err := s.Tenants().Insert(ctx, tn); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
		suspended := storetest.Tenant("initech")
		suspended.Status = tenant.StatusSuspended
		if err := s.Tenants().Insert(ctx, suspended); err != nil {
			t.Fatalf("insert: %v", err)
		}

		first, err := s.Tenants().List(ctx, &tenant.ListOptions{Limit: 2})
		if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
			t.Fatalf("first page = %v, %v", first, err)
		}
		rest, err := s.Tenants().List(ctx, &tenant.ListOptions{Limit: 10, Cursor: first.NextCursor})
		if err != nil || len(rest.Items) != 3 || rest.NextCursor != "" {
			t.Fatalf("second page = %v, %v", rest, err)
		}

		search := func(term string) []string {
			res, searchErr := s.Tenants().List(ctx, &tenant.ListOptions{Search: term})
			if searchErr != nil {
				t.Fatalf("search %q: %v", term, searchErr)
			}
			slugs := make([]string, 0, len(res.Items))
			for _, tn := range res.Items {
				slugs = append(slugs, tn.Slug)
			}
			slices.Sort(slugs)
			return slugs
		}
		if got := search("ACME"); !slices.Equal(got, []string{"acme-search", "acme_billing"}) {
			t.Fatalf("search ACME = %v", got)
		}
		// A wildcard in the term is a literal, not a pattern.
		if got := search("_"); !slices.Equal(got, []string{"acme_billing"}) {
			t.Fatalf("search _ = %v", got)
		}
		if got := search("%"); !slices.Equal(got, []string{"100%-off"}) {
			t.Fatalf("search %% = %v", got)
		}

		res, err := s.Tenants().List(ctx, &tenant.ListOptions{Status: string(tenant.StatusSuspended)})
		if err != nil || len(res.Items) != 1 || res.Items[0].Slug != "initech" {
			t.Fatalf("status filter = %v, %v", res, err)
		}
	})
}

func TestKeyListAcrossAndWithinTenants(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		a, b := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		ka, kb := storetest.Key(a.ID, "a-key"), storetest.Key(b.ID, "b-key")
		revoked := storetest.Key(a.ID, "a-old")
		revoked.Status = key.KeyRevoked
		for _, k := range []*key.APIKey{ka, kb, revoked} {
			if err := s.Keys().Insert(ctx, k); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
		all, err := s.Keys().List(ctx, &key.ListOptions{})
		if err != nil || len(all.Items) != 3 {
			t.Fatalf("every key = %v, %v", all, err)
		}
		active, err := s.Keys().List(ctx, &key.ListOptions{TenantID: a.ID.String(), Status: key.KeyActive})
		if err != nil || len(active.Items) != 1 || active.Items[0].ID.String() != ka.ID.String() {
			t.Fatalf("tenant A active = %v, %v", active, err)
		}
	})
}

func TestACursorFromAnotherListIsRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		for _, bad := range []string{"garbage", " ", id.NewTenantID().String()} {
			if _, err := s.Usage().Query(ctx, &usage.QueryOptions{Cursor: bad}); !errors.Is(err, paging.ErrInvalidCursor) {
				t.Errorf("usage cursor %q = %v, want ErrInvalidCursor", bad, err)
			}
		}
		if _, err := s.Tenants().List(ctx, &tenant.ListOptions{Cursor: id.NewUsageID().String()}); !errors.Is(err, paging.ErrInvalidCursor) {
			t.Errorf("tenant list with a usage cursor = %v, want ErrInvalidCursor", err)
		}
		if _, err := s.Keys().List(ctx, &key.ListOptions{Cursor: strings.Repeat("x", 30)}); !errors.Is(err, paging.ErrInvalidCursor) {
			t.Errorf("key list with garbage = %v, want ErrInvalidCursor", err)
		}
	})
}
