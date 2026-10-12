package postgres

import (
	"database/sql/driver"
	"fmt"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store/internal/conv"
)

// numeric carries an exact decimal to and from a NUMERIC column as text.
// pgx hands a NUMERIC to an sql.Scanner as its decimal text, so no float is
// ever involved. The zero value is NULL.
type numeric struct {
	text  string
	valid bool
}

func numericOf(c *money.USD) numeric {
	if s := conv.CostText(c); s != nil {
		return numeric{text: *s, valid: true}
	}
	return numeric{}
}

// Value implements driver.Valuer.
func (n numeric) Value() (driver.Value, error) {
	if !n.valid {
		return nil, nil
	}
	return n.text, nil
}

// Scan implements sql.Scanner.
func (n *numeric) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*n = numeric{}
	case string:
		*n = numeric{text: v, valid: true}
	case []byte:
		*n = numeric{text: string(v), valid: true}
	default:
		return fmt.Errorf("nexus/postgres: scan numeric from %T", src)
	}
	return nil
}

func (n numeric) usd() (*money.USD, error) {
	if !n.valid {
		return nil, nil
	}
	return conv.ParseCost(&n.text)
}
