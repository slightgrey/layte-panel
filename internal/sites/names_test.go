package sites

import "testing"

func TestMatchesServerName(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"panel.example.com", "panel.example.com", true},
		{"panel.example.com", "blog.example.com", false},
		{"*.example.com", "blog.example.com", true},
		{"*.example.com", "example.com", false},
		{".example.com", "example.com", true},
		{".example.com", "a.example.com", true},
		{".example.com", "badexample.com", false},
		{"www.example.*", "www.example.org", true},
		{"www.example.*", "blog.example.org", false},
		{`~^(www\.)?shop\.test$`, "shop.test", true},
		{`~^(www\.)?shop\.test$`, "blog.shop.test", false},
		{`~(?=bad)`, "anything.test", false}, // not valid in Go: left to nginx
	}
	for _, tt := range tests {
		if got := matchesServerName(tt.pattern, tt.name); got != tt.want {
			t.Errorf("matchesServerName(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}
