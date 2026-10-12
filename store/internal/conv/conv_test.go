package conv_test

import (
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store/internal/conv"
)

func TestOptionalIDs(t *testing.T) {
	if conv.OptionalID(id.Nil) != nil {
		t.Fatalf("a nil id should store as NULL")
	}
	tid := id.NewTenantID()
	s := conv.OptionalID(tid)
	got, err := conv.ParseOptional(s, id.ParseTenantID)
	if err != nil || got.String() != tid.String() {
		t.Fatalf("round trip = %s, %v", got, err)
	}
	for _, empty := range []*string{nil, new(string)} {
		if got, err := conv.ParseOptional(empty, id.ParseTenantID); err != nil || !got.IsNil() {
			t.Fatalf("empty = %s, %v", got, err)
		}
	}
}

func TestCostText(t *testing.T) {
	if conv.CostText(nil) != nil {
		t.Fatalf("an unknown cost should store as NULL")
	}
	c := money.MustParse("0.000000150")
	got, err := conv.ParseCost(conv.CostText(&c))
	if err != nil || got == nil || got.String() != "0.00000015" {
		t.Fatalf("round trip = %v, %v", got, err)
	}
	padded := "0.000000150000000000"
	if got, err := conv.ParseCost(&padded); err != nil || got.String() != "0.00000015" {
		t.Fatalf("a NUMERIC(38,18) text = %v, %v", got, err)
	}
}

func TestTimeTextSortsAsTime(t *testing.T) {
	a := time.Date(2026, 10, 7, 9, 0, 0, 5, time.FixedZone("x", -5*3600))
	b := a.Add(time.Nanosecond)
	if conv.TimeText(a) >= conv.TimeText(b) {
		t.Fatalf("%s should sort before %s", conv.TimeText(a), conv.TimeText(b))
	}
	got, err := conv.ParseTimeText(conv.TimeText(a))
	if err != nil || !got.Equal(a) {
		t.Fatalf("round trip = %s, %v", got, err)
	}
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	if got := conv.LikePattern(`50%_off\`); got != `%50\%\_off\\%` {
		t.Fatalf("LikePattern = %q", got)
	}
}
