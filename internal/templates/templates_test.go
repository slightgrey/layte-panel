package templates

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

// Run `go test ./internal/templates -update` to rewrite the golden files after
// an intentional template change, then review the diff.
var update = flag.Bool("update", false, "rewrite testdata/*.golden")

func TestGolden(t *testing.T) {
	site := Site{Domain: "example.com", Aliases: []string{"www.example.com"}, User: "examplecom", PHPVersion: "8.3"}
	tests := []struct {
		golden string
		render func(Site) ([]byte, error)
	}{
		{"nginx-site.golden", Vhost},
		{"fpm-pool.golden", Pool},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			got, err := tt.render(site)
			if err != nil {
				t.Fatal(err)
			}
			if !system.IsManaged(got) {
				t.Error("rendered file lacks the Layte marker")
			}
			path := filepath.Join("testdata", tt.golden)
			if *update {
				os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from golden file; run with -update if intended.\n--- got ---\n%s", tt.golden, got)
			}
		})
	}
}

func TestPaths(t *testing.T) {
	s := Site{Domain: "a.test", User: "atest", PHPVersion: "8.2"}
	if s.Dir() != "/home/atest/a.test" || s.Socket() != "/run/php/layte-a.test-php8.2.sock" {
		t.Errorf("dir=%s socket=%s", s.Dir(), s.Socket())
	}
	if PoolPath("8.2", "a.test") != "/etc/php/8.2/fpm/pool.d/layte-a.test.conf" {
		t.Error(PoolPath("8.2", "a.test"))
	}
	if _, err := Vhost(Site{Domain: "x.test"}); err == nil {
		t.Error("incomplete site rendered")
	}
}
