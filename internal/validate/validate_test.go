package validate

import "testing"

func TestDomain(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"example.com", "example.com", true},
		{"Example.COM.", "example.com", true},
		{"www.my-shop.co.uk", "www.my-shop.co.uk", true},
		{"bücher.example", "xn--bcher-kva.example", true},
		{"localhost", "", false},
		{"127.0.0.1", "", false},
		{"::1", "", false},
		{"../etc/passwd", "", false},
		{"a..b.com", "", false},
		{"ex/ample.com", "", false},
		{"*.example.com", "", false},
		{"-bad.com", "", false},
		{"bad-.com", "", false},
		{"under_score.com", "", false},
		{"example.123", "", false},
		{"exa mple.com", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := Domain(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("Domain(%q) = %q, %v; want %q ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestDomains(t *testing.T) {
	if _, err := Domains("a.com", []string{"www.a.com", "WWW.a.com"}); err == nil {
		t.Error("duplicate alias accepted")
	}
	if _, err := Domains("a.com", []string{"a.com"}); err == nil {
		t.Error("primary as alias accepted")
	}
	got, err := Domains("a.com", []string{"www.a.com"})
	if err != nil || len(got) != 1 {
		t.Errorf("got %v %v", got, err)
	}
}

func TestDefaultUsername(t *testing.T) {
	tests := map[string]string{
		"www.my-shop.co.uk":                      "myshopcouk",
		"example.com":                            "examplecom",
		"1password.com":                          "site1passwordcom",
		"a.io":                                   "aio",
		"very-long-domain-name-here.example.com": "verylongdomainnamehereex",
	}
	for in, want := range tests {
		got := DefaultUsername(in)
		if got != want {
			t.Errorf("DefaultUsername(%q) = %q, want %q", in, got, want)
		}
		if err := SiteUsername(got); err != nil {
			t.Errorf("DefaultUsername(%q) = %q is not a valid username: %v", in, got, err)
		}
	}
}

func TestSiteUsername(t *testing.T) {
	for _, u := range []string{"root", "www-data", "layte", "systemd-network"} {
		if SiteUsername(u) == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if err := SiteUsername("acme"); err != nil {
		t.Error(err)
	}
}

func TestDBIdentPasswordEmailPHP(t *testing.T) {
	if DBIdent("wp_shop") != nil || DBIdent("x") != nil {
		t.Error("valid ident rejected")
	}
	for _, s := range []string{"", "1abc", "a-b", "a`b", "a'b", "drop table;", "ABC"} {
		if DBIdent(s) == nil {
			t.Errorf("DBIdent(%q) accepted", s)
		}
	}
	if Password("abcDEF0123456789abcdefGH") != nil {
		t.Error("valid password rejected")
	}
	if Password("short") == nil || Password("has'quote-and-is-long-enough-000") == nil {
		t.Error("bad password accepted")
	}
	if Email("me@example.com") != nil || Email("Me <me@example.com>") == nil || Email("nope") == nil {
		t.Error("email rules")
	}
	if PHPVersion("8.3", []string{"8.2", "8.3"}) != nil || PHPVersion("7.4", []string{"8.3"}) == nil || PHPVersion("8.3;rm", []string{"8.3;rm"}) == nil {
		t.Error("php version rules")
	}
	if Title("My shop") != nil || Title("bad\nline") == nil {
		t.Error("title rules")
	}
}
