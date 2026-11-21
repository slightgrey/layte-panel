package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		check   func(Config) bool
		wantErr bool
	}{
		{"listen", map[string]string{"LAYTE_LISTEN": "0.0.0.0:9000"}, func(c Config) bool { return c.Listen == "0.0.0.0:9000" }, false},
		{"proxies", map[string]string{"LAYTE_TRUSTED_PROXIES": "10.0.0.1, 10.0.0.2"}, func(c Config) bool {
			return len(c.TrustedProxies) == 2 && c.TrustedProxies[1] == "10.0.0.2"
		}, false},
		{"duration", map[string]string{"LAYTE_SESSION_IDLE": "5m"}, func(c Config) bool { return c.SessionIdle.Duration == 5*time.Minute }, false},
		{"bad duration", map[string]string{"LAYTE_SESSION_IDLE": "soon"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			err := applyEnv(&c, func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil && !tt.check(c) {
				t.Errorf("unexpected config: %+v", c)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		ok     bool
	}{
		{"defaults", func(*Config) {}, true},
		{"bad listen", func(c *Config) { c.Listen = "nope" }, false},
		{"bad port", func(c *Config) { c.Listen = "127.0.0.1:99999" }, false},
		{"bad proxy", func(c *Config) { c.TrustedProxies = []string{"example.com"} }, false},
		{"idle > max", func(c *Config) { c.SessionIdle.Duration = 24 * time.Hour }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			tt.mutate(&c)
			if err := c.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("listen = \"127.0.0.1:9443\"\nsession_idle = \"10m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:9443" || c.SessionIdle.Duration != 10*time.Minute || !c.Dev {
		t.Errorf("got %+v", c)
	}
	// A missing file is fine: defaults are used.
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml"), false); err != nil {
		t.Errorf("missing file: %v", err)
	}
}
