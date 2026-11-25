package redirects

import "testing"

func TestClean(t *testing.T) {
	got, err := Clean([]Redirect{
		{From: " /hire-vince/ ", To: "/hire/"},
		{From: "/old", To: "https://example.com/new?a=1&b=2", Code: 302},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != (Redirect{From: "/hire-vince", To: "/hire/", Code: 301}) || got[1].Code != 302 {
		t.Errorf("got %+v", got)
	}
	if err := IsClean(got); err != nil {
		t.Errorf("cleaned list not clean: %v", err)
	}
	if IsClean([]Redirect{{From: "/a/", To: "/b"}}) == nil {
		t.Error("unnormalised list accepted by IsClean")
	}
	if l, err := Clean(nil); err != nil || l != nil {
		t.Errorf("empty: %v %v", l, err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Redirect{
		{From: "/", To: "/x"},
		{From: "", To: "/x"},
		{From: "old", To: "/x"},
		{From: "/a b", To: "/x"},
		{From: "/a;", To: "/x"},
		{From: "/a{", To: "/x"},
		{From: "/a$uri", To: "/x"},
		{From: "/%2e%2e", To: "/x"},
		{From: "/a/../b", To: "/x"},
		{From: "/a//b", To: "/x"},
		{From: "/wp-config.php", To: "/x"},
		{From: "/.well-known/acme-challenge/x", To: "/x"},
		{From: "/a", To: "/b", Code: 307},
		{From: "/a", To: ""},
		{From: "/a", To: "/b; return 200"},
		{From: "/a", To: "/b$request_uri"},
		{From: "/a", To: `/b"`},
		{From: "/a", To: "/b{"},
		{From: "/a", To: "//evil.example"},
		{From: "/a", To: "javascript:alert(1)"},
		{From: "/a", To: "ftp://example.com/"},
		{From: "/a", To: "https://user:pw@example.com/"},
		{From: "/a", To: "/a/"}, // loop
		{From: "/a", To: "/a?x=1"},
	}
	for _, r := range bad {
		if r.Normalize().Validate() == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	good := []Redirect{
		{From: "/hire-vince", To: "/hire/"},
		{From: "/a/b.html", To: "/c"},
		{From: "/blog/2019/post", To: "https://new.example.com/post", Code: 302},
		{From: "/x", To: "/search?q=a+b&page=2"},
	}
	for _, r := range good {
		if err := r.Normalize().Validate(); err != nil {
			t.Errorf("rejected %+v: %v", r, err)
		}
	}
	if _, err := Clean([]Redirect{{From: "/a", To: "/b"}, {From: "/a/", To: "/c"}}); err == nil {
		t.Error("same path twice accepted")
	}
	many := make([]Redirect, Max+1)
	if _, err := Clean(many); err == nil {
		t.Error("too many accepted")
	}
}
