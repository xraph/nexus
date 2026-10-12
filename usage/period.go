package usage

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidPeriod reports a period other than day, week or month.
var ErrInvalidPeriod = errors.New("nexus: period must be day, week or month")

// PeriodStart is the UTC instant a period began as of now: midnight UTC for
// "day", seven days before now for "week", the first of the month at
// midnight UTC for "month". Every backend uses it, so none computes "now"
// in its own time zone.
func PeriodStart(period string, now time.Time) (time.Time, error) {
	now = now.UTC()
	switch period {
	case "day":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	case "week":
		return now.Add(-7 * 24 * time.Hour), nil
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidPeriod, period)
}
