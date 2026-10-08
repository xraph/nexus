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
	"2006-01-02 15:04:05.999999999 -0700",     // time.Time.String with its zone name dropped
	"2006-01-02 15:04:05.999999999-07:00",     // modernc's own format
	time.RFC3339Nano,
	"2006-01-02 15:04:05", // datetime('now'), which is UTC
}

// zoneNameSuffix is the zone abbreviation time.Time.String appends after the
// numeric offset. The offset already says everything, and Go cannot parse an
// abbreviation it does not know, so it is dropped before parsing.
var zoneNameSuffix = regexp.MustCompile(`( [+-]\d{4}) \S+$`)

// parseLegacyTime reads any form a time column held before conv.TimeText.
func parseLegacyTime(s string) (time.Time, error) {
	s = monotonicSuffix.ReplaceAllString(s, "")
	s = zoneNameSuffix.ReplaceAllString(s, "$1")
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

// parseKeyExpiry reads api_keys.expires_at: conv.TimeText, or any form a key
// held before it (Migrate rewrites those, so this only matters for a row
// written by an older binary after the migration).
func parseKeyExpiry(s string) (time.Time, error) {
	if t, err := conv.ParseTimeText(s); err == nil {
		return t, nil
	}
	return parseLegacyTime(s)
}

// normaliseKeyExpiry rewrites every expires_at that is not already
// conv.TimeText, so the key status filter compares it as text exactly as it
// would as a time. Before this, grove wrote a time.Time with its own zone, in
// time.Time.String form. It touches nothing once every row is in that form.
func (s *Store) normaliseKeyExpiry(ctx context.Context) error {
	rows, err := s.sdb.Query(ctx,
		`SELECT id, expires_at FROM api_keys
		  WHERE expires_at IS NOT NULL
		    AND (length(expires_at) != 30 OR substr(expires_at, 11, 1) != 'T' OR substr(expires_at, 30, 1) != 'Z')`)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: find legacy key expiries: %w", err)
	}
	type fix struct{ id, expires string }
	var fixes []fix
	for rows.Next() {
		var rowID, expires string
		if scanErr := rows.Scan(&rowID, &expires); scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("nexus/sqlite: scan legacy key expiry: %w", scanErr)
		}
		t, parseErr := parseLegacyTime(expires)
		if parseErr != nil {
			_ = rows.Close()
			return fmt.Errorf("nexus/sqlite: key %s expires_at: %w", rowID, parseErr)
		}
		fixes = append(fixes, fix{rowID, conv.TimeText(t)})
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return fmt.Errorf("nexus/sqlite: read legacy key expiries: %w", err)
	}
	for _, f := range fixes {
		if _, err := s.sdb.Exec(ctx, `UPDATE api_keys SET expires_at = ? WHERE id = ?`, f.expires, f.id); err != nil {
			return fmt.Errorf("nexus/sqlite: normalise key %s expires_at: %w", f.id, err)
		}
	}
	return nil
}
