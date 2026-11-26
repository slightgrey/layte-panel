package actions

import "testing"

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"1.18.0", false}, {"1.24.0", false}, {"1.25.0", false}, {"1.25.1", true},
		{"1.26.3", true}, {"1.28.0", true}, {"2.0", true}, {"", false}, {"x.y", false},
	}
	for _, tt := range tests {
		if got := versionAtLeast(tt.v, 1, 25, 1); got != tt.want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
