package pagecache

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, ttl := range []int{0, 1, 60, 1440} {
		if err := (Settings{Enabled: true, TTLMinutes: ttl}).Validate(); err != nil {
			t.Errorf("%d: %v", ttl, err)
		}
	}
	for _, ttl := range []int{-1, 1441} {
		if err := (Settings{TTLMinutes: ttl}).Validate(); err == nil {
			t.Errorf("%d accepted", ttl)
		}
	}
}

func TestZone(t *testing.T) {
	a, b := Zone("a-b.com"), Zone("a.b.com")
	if a == b || !strings.HasPrefix(a, "layte_a_b_com_") {
		t.Errorf("zones %q %q", a, b)
	}
}
