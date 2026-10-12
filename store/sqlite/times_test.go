package sqlite

import (
	"testing"
	"time"
)

func TestParseLegacyTime(t *testing.T) {
	cdt := time.FixedZone("CDT", -5*3600)
	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{"time.String UTC", "2026-10-07 15:47:16.805728 +0000 UTC", time.Date(2026, 10, 7, 15, 47, 16, 805728000, time.UTC)},
		{"time.String offset with monotonic suffix", "2026-10-07 10:47:16.805728 -0500 CDT m=+0.028456085", time.Date(2026, 10, 7, 10, 47, 16, 805728000, cdt)},
		{"time.String negative monotonic", "2026-10-07 10:47:16.5 -0500 CDT m=-0.000123", time.Date(2026, 10, 7, 10, 47, 16, 500000000, cdt)},
		{"time.String no fraction", "2026-10-07 15:47:16 +0530 IST", time.Date(2026, 10, 7, 15, 47, 16, 0, time.FixedZone("IST", 5*3600+1800))},
		{"zero time", "0001-01-01 00:00:00 +0000 UTC", time.Time{}},
		{"modernc format", "2026-10-07 10:47:16.805728123-05:00", time.Date(2026, 10, 7, 10, 47, 16, 805728123, cdt)},
		{"rfc3339", "2026-10-01T10:00:00Z", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{"rfc3339 nano offset", "2026-10-01T05:00:00.123456789-05:00", time.Date(2026, 10, 1, 10, 0, 0, 123456789, time.UTC)},
		{"datetime now", "2026-10-01 10:00:00", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLegacyTime(tc.in)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parse %q = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
	for _, junk := range []string{"", "yesterday", "2026-13-45 99:99:99", "2026-10-07"} {
		if got, err := parseLegacyTime(junk); err == nil {
			t.Errorf("parse %q = %s, want an error", junk, got)
		}
	}
}
