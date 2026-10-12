// Package paging holds the cursor rules every Nexus list shares. Lists are
// ordered newest first by ID (IDs are UUIDv7 TypeIDs, so their text sorts in
// creation order), and a page is continued from the last ID it returned.
// There are no totals: a total on a growing list is expensive and usually
// an estimate.
package paging

import (
	"errors"
	"fmt"

	"github.com/xraph/nexus/id"
)

const (
	// DefaultLimit is the page size when none is asked for.
	DefaultLimit = 50
	// MaxLimit is the largest page a store returns.
	MaxLimit = 500
)

// ErrInvalidCursor reports a cursor that is not an ID from the list it was
// handed to. It is never treated as "start again from the top".
var ErrInvalidCursor = errors.New("nexus: invalid cursor")

// Limit clamps a requested page size.
func Limit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

// CheckCursor accepts "" (the first page) or an ID of the given kind.
func CheckCursor(cursor string, prefix id.Prefix) error {
	if cursor == "" {
		return nil
	}
	if _, err := id.ParseWithPrefix(cursor, prefix); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidCursor, cursor)
	}
	return nil
}

// Trim takes the limit+1 rows a store fetched and returns the page and the
// cursor for the next one, or "" when this is the last page.
func Trim[T any](rows []T, limit int, idOf func(T) string) (page []T, next string) {
	if len(rows) <= limit {
		return rows, ""
	}
	page = rows[:limit]
	return page, idOf(page[limit-1])
}
