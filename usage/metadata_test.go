package usage_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xraph/nexus/usage"
)

func TestValidateMetadata(t *testing.T) {
	many := map[string]string{}
	for i := range usage.MaxMetadataPairs + 1 {
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	ok := map[string]map[string]string{
		"nil":   nil,
		"empty": {},
		"pairs": {"project": "p1", "run": "r_1"},
	}
	for name, m := range ok {
		if err := usage.ValidateMetadata(m); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]map[string]string{
		"17 pairs":    many,
		"dotted key":  {"a.b": "x"},
		"dollar key":  {"$x": "x"},
		"empty key":   {"": "x"},
		"65 char key": {strings.Repeat("k", 65): "x"},
		"long value":  {"a": strings.Repeat("v", 257)},
	}
	for name, m := range bad {
		if err := usage.ValidateMetadata(m); !errors.Is(err, usage.ErrInvalidMetadata) {
			t.Errorf("%s: err = %v, want ErrInvalidMetadata", name, err)
		}
	}
	if err := usage.ValidateMetadata(map[string]string{strings.Repeat("k", 64): strings.Repeat("v", 256)}); err != nil {
		t.Errorf("limits: %v", err)
	}
}
