// Package conv holds the conversions every database backend uses at its
// model boundary, so the backends cannot each invent their own.
package conv

import (
	"strings"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
)

// OptionalID stores id.Nil as NULL.
func OptionalID(i id.ID) *string {
	if i.IsNil() {
		return nil
	}
	s := i.String()
	return &s
}

// ParseOptional reads NULL and "" as id.Nil. Rows written before attribution
// was nullable hold "" for "nobody".
func ParseOptional(s *string, parse func(string) (id.ID, error)) (id.ID, error) {
	if s == nil || *s == "" {
		return id.Nil, nil
	}
	return parse(*s)
}

// CostText stores an unknown cost as NULL and a known one as its exact text.
func CostText(c *money.USD) *string {
	if c == nil {
		return nil
	}
	s := c.String()
	return &s
}

// ParseCost reads NULL as an unknown cost. It accepts exponent notation
// because some databases print decimals that way.
func ParseCost(s *string) (*money.USD, error) {
	if s == nil {
		return nil, nil
	}
	u, err := money.ParseLenient(*s)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// timeLayout is fixed width with nanoseconds, so comparing two values as
// text compares them as times.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// TimeText formats t in UTC for a text column that is compared and ordered.
func TimeText(t time.Time) string { return t.UTC().Format(timeLayout) }

// ParseTimeText reads TimeText's format.
func ParseTimeText(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// LikePattern turns a search term into a LIKE / ILIKE pattern matching it
// anywhere, with the wildcards in the term escaped by a backslash.
func LikePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}
