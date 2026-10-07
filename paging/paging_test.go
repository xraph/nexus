package paging_test

import (
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/paging"
)

func TestLimit(t *testing.T) {
	for in, want := range map[int]int{-1: 50, 0: 50, 1: 1, 500: 500, 501: 500} {
		if got := paging.Limit(in); got != want {
			t.Errorf("Limit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestCheckCursor(t *testing.T) {
	if err := paging.CheckCursor("", id.PrefixUsage); err != nil {
		t.Fatalf("empty cursor is the first page, got %v", err)
	}
	if err := paging.CheckCursor(id.NewUsageID().String(), id.PrefixUsage); err != nil {
		t.Fatalf("a usage id is a usage cursor, got %v", err)
	}
	for _, bad := range []string{"x", " ", id.NewTenantID().String(), "usage_"} {
		if err := paging.CheckCursor(bad, id.PrefixUsage); !errors.Is(err, paging.ErrInvalidCursor) {
			t.Errorf("CheckCursor(%q) = %v, want ErrInvalidCursor", bad, err)
		}
	}
}

func TestTrim(t *testing.T) {
	ids := func(s string) string { return s }
	page, next := paging.Trim([]string{"c", "b", "a"}, 2, ids)
	if len(page) != 2 || next != "b" {
		t.Fatalf("Trim over limit = %v, %q", page, next)
	}
	page, next = paging.Trim([]string{"c", "b"}, 2, ids)
	if len(page) != 2 || next != "" {
		t.Fatalf("Trim at limit = %v, %q", page, next)
	}
}
