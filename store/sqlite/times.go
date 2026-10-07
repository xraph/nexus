package sqlite

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/xraph/nexus/store/internal/conv"
)

// monotonicSuffix is what time.Time.String appends when the value carries a
// monotonic clock reading.
var monotonicSuffix = regexp.MustCompile(` m=[+-][0-9.]+$`)

// legacyTimeLayouts are the forms usage_records.created_at held before it
// became conv.TimeText, in the order they are tried. The first is what
// modernc wrote when the old store bound a time.Time with no _time_format
// DSN parameter.
var legacyTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST", // time.Time.String
	"2006-01-02 15:04:05.999999999-07:00",     // modernc's own format
	time.RFC3339Nano,
	"2006-01-02 15:04:05", // datetime('now'), which is UTC
}

// parseLegacyTime reads any form created_at held before conv.TimeText.
func parseLegacyTime(s string) (time.Time, error) {
	s = monotonicSuffix.ReplaceAllString(s, "")
	for _, layout := range legacyTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", s)
}

// normaliseUsageTimes rewrites every created_at that is not already
// conv.TimeText, so a time window compares as text exactly as it would as a
// time. It touches nothing once every row is in that form.
func (s *Store) normaliseUsageTimes(ctx context.Context) error {
	rows, err := s.sdb.Query(ctx,
		`SELECT id, created_at FROM usage_records
		  WHERE length(created_at) != 30 OR substr(created_at, 11, 1) != 'T' OR substr(created_at, 30, 1) != 'Z'`)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: find legacy usage times: %w", err)
	}
	type fix struct{ id, created string }
	var fixes []fix
	for rows.Next() {
		var rowID, created string
		if scanErr := rows.Scan(&rowID, &created); scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("nexus/sqlite: scan legacy usage time: %w", scanErr)
		}
		t, parseErr := parseLegacyTime(created)
		if parseErr != nil {
			_ = rows.Close()
			return fmt.Errorf("nexus/sqlite: usage record %s created_at: %w", rowID, parseErr)
		}
		fixes = append(fixes, fix{rowID, conv.TimeText(t)})
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return fmt.Errorf("nexus/sqlite: read legacy usage times: %w", err)
	}
	for _, f := range fixes {
		if _, err := s.sdb.Exec(ctx, `UPDATE usage_records SET created_at = ? WHERE id = ?`, f.created, f.id); err != nil {
			return fmt.Errorf("nexus/sqlite: normalise usage record %s created_at: %w", f.id, err)
		}
	}
	return nil
}
