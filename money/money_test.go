package money_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/nexus/money"
)

func TestParseRefusesAnythingButAPlainDecimal(t *testing.T) {
	for _, s := range []string{"", "-", ".", "1e-7", "1E3", "NaN", "Inf", "+1", "1,000", " 1", "1 ", "0x10", "1.2.3", "$1"} {
		if _, err := money.Parse(s); !errors.Is(err, money.ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", s, err)
		}
	}
}

func TestStringIsExactAndCanonical(t *testing.T) {
	cases := map[string]string{
		"0.15":            "0.15",
		"10.00":           "10",
		"0.000000150":     "0.00000015",
		"-3":              "-3",
		"0":               "0",
		".5":              "0.5",
		"1284.3702190000": "1284.370219",
	}
	for in, want := range cases {
		if got := money.MustParse(in).String(); got != want {
			t.Errorf("MustParse(%q).String() = %q, want %q", in, got, want)
		}
	}
}

func TestPerMillionIsExact(t *testing.T) {
	cases := []struct {
		price  string
		tokens int64
		want   string
	}{
		{"0.15", 1, "0.00000015"},
		{"2.50", 1_000_000, "2.5"},
		{"0.075", 333, "0.000024975"},
		{"0.00001", 7, "0.00000000007"},
	}
	for _, c := range cases {
		if got := money.MustParse(c.price).PerMillion(c.tokens).String(); got != c.want {
			t.Errorf("%s per million × %d = %s, want %s", c.price, c.tokens, got, c.want)
		}
	}
}

func TestManySubCentAmountsSumWithoutDrift(t *testing.T) {
	total := money.Zero
	for range 1000 {
		total = total.Add(money.MustParse("0.000000150"))
	}
	if total.String() != "0.00015" {
		t.Fatalf("1000 × 0.000000150 = %s, want 0.00015", total)
	}
	if got := money.Sum(money.MustParse("0.1"), money.MustParse("0.2")).String(); got != "0.3" {
		t.Fatalf("0.1 + 0.2 = %s, want 0.3", got)
	}
}

func TestComparisons(t *testing.T) {
	a, b := money.MustParse("1.10"), money.MustParse("1.1")
	if !a.Equal(b) || a.Cmp(b) != 0 {
		t.Fatalf("1.10 and 1.1 should be equal")
	}
	if !money.MustParse("0.01").IsPositive() || !money.MustParse("-0.01").IsNegative() || !money.Zero.IsZero() {
		t.Fatalf("sign predicates are wrong")
	}
	if got := money.MustParse("0.03").Mul(3).Sub(money.MustParse("0.09")); !got.IsZero() {
		t.Fatalf("0.03 × 3 - 0.09 = %s, want 0", got)
	}
}

func TestParseLenientReadsExponentsExactly(t *testing.T) {
	got, err := money.ParseLenient("1.50E-7")
	if err != nil || got.String() != "0.00000015" {
		t.Fatalf("ParseLenient(1.50E-7) = %s, %v", got, err)
	}
	if _, err := money.ParseLenient("NaN"); !errors.Is(err, money.ErrInvalid) {
		t.Fatalf("ParseLenient(NaN) err = %v, want ErrInvalid", err)
	}
}

func TestParseLenientRefusesUnboundedExponents(t *testing.T) {
	if _, err := money.ParseLenient("1e999999999"); !errors.Is(err, money.ErrInvalid) {
		t.Errorf("ParseLenient(1e999999999) err = %v, want ErrInvalid", err)
	}
	if _, err := money.ParseLenient("1e-999999999"); !errors.Is(err, money.ErrInvalid) {
		t.Errorf("ParseLenient(1e-999999999) err = %v, want ErrInvalid", err)
	}
}

func TestParseRefusesLongStrings(t *testing.T) {
	longStr := "1" + strings.Repeat("1", 80)
	if _, err := money.Parse(longStr); !errors.Is(err, money.ErrInvalid) {
		t.Errorf("Parse(81-char string) err = %v, want ErrInvalid", err)
	}
}

func TestJSON(t *testing.T) {
	type doc struct {
		Price money.USD  `json:"price"`
		Cost  *money.USD `json:"cost"`
	}
	b, err := json.Marshal(doc{Price: money.MustParse("0.15")})
	if err != nil || string(b) != `{"price":"0.15","cost":null}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}

	var d doc
	if err := json.Unmarshal([]byte(`{"price":12.5,"cost":"0.000024975"}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Price.String() != "12.5" || d.Cost == nil || d.Cost.String() != "0.000024975" {
		t.Fatalf("unmarshal = %s, %v", d.Price, d.Cost)
	}

	// A legacy float written by encoding/json comes back exactly from its text.
	if err := json.Unmarshal([]byte(`{"price":1e-07}`), &d); err != nil || d.Price.String() != "0.0000001" {
		t.Fatalf("bare exponent number = %s, %v", d.Price, err)
	}

	// A string must be a plain decimal.
	if err := json.Unmarshal([]byte(`{"price":"1e-7"}`), &d); err == nil {
		t.Fatalf("a string in exponent form should be refused")
	}

	// null leaves the value alone.
	d.Price = money.MustParse("3")
	if err := json.Unmarshal([]byte(`{"price":null}`), &d); err != nil || d.Price.String() != "3" {
		t.Fatalf("null = %s, %v", d.Price, err)
	}

	// Unbounded exponents in bare numbers are rejected.
	if err := json.Unmarshal([]byte(`{"price":1e999999999}`), &d); err == nil {
		t.Fatalf("unbounded exponent in bare number should be refused")
	}

	// High-precision floats come back exactly from their text representation.
	if err := json.Unmarshal([]byte(`{"price":0.30000000000000004}`), &d); err != nil || d.Price.String() != "0.30000000000000004" {
		t.Fatalf("0.30000000000000004 = %s, %v", d.Price, err)
	}
	if err := json.Unmarshal([]byte(`{"price":-0.1}`), &d); err != nil || d.Price.String() != "-0.1" {
		t.Fatalf("-0.1 = %s, %v", d.Price, err)
	}
	if err := json.Unmarshal([]byte(`{"price":1234567890.1234567891}`), &d); err != nil || d.Price.String() != "1234567890.1234567891" {
		t.Fatalf("20-digit value = %s, %v", d.Price, err)
	}
}

func TestAmountsCarryAtMostEighteenPlaces(t *testing.T) {
	for _, s := range []string{"0.000000000000000001", "1.500000000000000000"} {
		if _, err := money.Parse(s); err != nil {
			t.Errorf("Parse(%q) = %v, want it accepted", s, err)
		}
	}
	for _, s := range []string{"0.0000000000000000001", "1.5000000000000000000"} {
		if _, err := money.Parse(s); !errors.Is(err, money.ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", s, err)
		}
	}
	if _, err := money.ParseLenient("1E-18"); err != nil {
		t.Errorf("ParseLenient(1E-18) = %v, want it accepted", err)
	}
	for _, s := range []string{"1E-19", "0.0000000000000000001", "1.5E-19"} {
		if _, err := money.ParseLenient(s); !errors.Is(err, money.ErrInvalid) {
			t.Errorf("ParseLenient(%q) err = %v, want ErrInvalid", s, err)
		}
	}
	var u money.USD
	if err := json.Unmarshal([]byte(`"0.0000000000000000001"`), &u); !errors.Is(err, money.ErrInvalid) {
		t.Errorf("UnmarshalJSON of 19 places err = %v, want ErrInvalid", err)
	}
}

func TestPerMillionRoundsToMaxPlaces(t *testing.T) {
	cases := []struct {
		price  string
		tokens int64
		want   string
	}{
		{"0.000000000000000001", 3, "0"},                          // 3e-24 rounds to 0
		{"0.000000000000000009", 500_000, "0.000000000000000005"}, // 4.5e-18 rounds half away from zero
		{"0.000000000000000009", 400_000, "0.000000000000000004"}, // 3.6e-18
		{"0.075", 333, "0.000024975"},                             // unaffected
	}
	for _, c := range cases {
		got := money.MustParse(c.price).PerMillion(c.tokens)
		if got.String() != c.want {
			t.Errorf("%s per million × %d = %s, want %s", c.price, c.tokens, got, c.want)
		}
		if _, err := money.Parse(got.String()); err != nil {
			t.Errorf("computed %s does not parse back: %v", got, err)
		}
	}
}
