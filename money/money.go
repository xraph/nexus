// Package money holds exact amounts of US dollars. Nothing in it rounds:
// rounding is a display decision, made where an amount is shown.
package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// USD is an exact decimal amount of US dollars. The zero value is $0.
type USD struct{ d decimal.Decimal }

// Zero is $0.
var Zero USD

// ErrInvalid reports a string that is not an amount.
var ErrInvalid = errors.New("money: invalid amount")

// Parse reads a plain decimal such as "0.15", ".5" or "-3". It refuses
// exponents, NaN, Inf, a leading plus sign, separators and spaces, so an
// amount someone typed is read the way they typed it.
func Parse(s string) (USD, error) {
	if len(s) > 80 {
		return Zero, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	if !plainDecimal(s) {
		return Zero, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	return parse(s)
}

// ParseLenient also accepts exponent notation ("1.50E-7"). It exists for
// values read back from a database or an old JSON document that prints
// decimals that way. The result is still exact.
func ParseLenient(s string) (USD, error) {
	if len(s) > 80 {
		return Zero, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	if err := checkExponentBound(s); err != nil {
		return Zero, err
	}
	return parse(s)
}

// MustParse is Parse for literals. It panics on a malformed amount.
func MustParse(s string) USD {
	u, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func parse(s string) (USD, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Zero, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	return USD{d: d}, nil
}

func plainDecimal(s string) bool {
	if s == "" {
		return false
	}
	i, digits, dot := 0, 0, false
	if s[0] == '-' {
		i = 1
	}
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return digits > 0
}

func checkExponentBound(s string) error {
	for i, c := range s {
		if c == 'e' || c == 'E' {
			expStr := s[i+1:]
			var expVal int64
			if _, err := fmt.Sscanf(expStr, "%d", &expVal); err == nil {
				if expVal > 40 || expVal < -40 {
					return fmt.Errorf("%w: %q", ErrInvalid, s)
				}
			}
			break
		}
	}
	return nil
}

// Sum adds amounts exactly.
func Sum(vs ...USD) USD {
	total := Zero
	for _, v := range vs {
		total = total.Add(v)
	}
	return total
}

// Add returns u + v.
func (u USD) Add(v USD) USD { return USD{d: u.d.Add(v.d)} }

// Sub returns u - v.
func (u USD) Sub(v USD) USD { return USD{d: u.d.Sub(v.d)} }

// Mul multiplies by a whole count.
func (u USD) Mul(n int64) USD { return USD{d: u.d.Mul(decimal.NewFromInt(n))} }

// PerMillion is the cost of n units at u per million units: u × n / 10⁶.
func (u USD) PerMillion(n int64) USD {
	return USD{d: u.d.Mul(decimal.NewFromInt(n)).Shift(-6)}
}

// Cmp returns -1, 0 or +1 as u is less than, equal to or greater than v.
func (u USD) Cmp(v USD) int { return u.d.Cmp(v.d) }

// Equal reports whether u and v are the same amount (1.10 equals 1.1).
func (u USD) Equal(v USD) bool { return u.d.Equal(v.d) }

// IsZero reports whether u is $0.
func (u USD) IsZero() bool { return u.d.IsZero() }

// IsPositive reports whether u is more than $0.
func (u USD) IsPositive() bool { return u.d.IsPositive() }

// IsNegative reports whether u is less than $0.
func (u USD) IsNegative() bool { return u.d.IsNegative() }

// String is the exact amount in plain decimal notation with no trailing
// zeros: "0.15", "1284.370219", "0".
func (u USD) String() string { return u.d.String() }

// MarshalJSON writes the amount as a JSON string, so no reader can turn it
// into a float on the way in.
func (u USD) MarshalJSON() ([]byte, error) { return json.Marshal(u.String()) }

// UnmarshalJSON reads a JSON string strictly, a bare JSON number leniently
// and exactly from its text (budgets were stored as numbers before amounts
// were strings), and treats null as "leave the value alone".
func (u *USD) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v, err := Parse(s)
		if err != nil {
			return err
		}
		*u = v
		return nil
	}
	v, err := ParseLenient(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}
