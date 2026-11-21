// Package config loads Layte Panel's configuration from a TOML file plus
// LAYTE_* environment variable overrides.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPath is where the production config file lives.
const DefaultPath = "/etc/layte/config.toml"

// Config holds every setting for both `serve` and `agent`.
type Config struct {
	Listen         string   `toml:"listen"`
	DataDir        string   `toml:"data_dir"`
	AgentSocket    string   `toml:"agent_socket"`
	SecretKeyFile  string   `toml:"secret_key_file"`
	TrustedProxies []string `toml:"trusted_proxies"`
	SessionIdle    Duration `toml:"session_idle"`
	SessionMax     Duration `toml:"session_max"`

	// Dev is set by the --dev flag, never by the file.
	Dev bool `toml:"-"`
	// DevOrigin is the Vite dev server origin allowed for CORS in dev mode.
	DevOrigin string `toml:"dev_origin"`
}

// Duration wraps time.Duration so it can be written as "30m" in TOML.
type Duration struct{ time.Duration }

// UnmarshalText is called by the TOML decoder (it implements encoding.TextUnmarshaler).
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Defaults returns the production defaults.
func Defaults() Config {
	return Config{
		Listen:         "127.0.0.1:8443",
		DataDir:        "/var/lib/layte",
		AgentSocket:    "/run/layte/agent.sock",
		SecretKeyFile:  "/etc/layte/secret.key",
		TrustedProxies: []string{"127.0.0.1", "::1"},
		SessionIdle:    Duration{30 * time.Minute},
		SessionMax:     Duration{12 * time.Hour},
		DevOrigin:      "http://localhost:5173",
	}
}

// DevDefaults keeps all state under ./.dev so nothing touches system paths.
func DevDefaults() Config {
	c := Defaults()
	c.Dev = true
	c.DataDir = filepath.Join(".dev", "data")
	c.AgentSocket = filepath.Join(".dev", "agent.sock")
	c.SecretKeyFile = filepath.Join(".dev", "secret.key")
	return c
}

// Load reads defaults, then the file at path (if it exists), then env overrides.
func Load(path string, dev bool) (Config, error) {
	c := Defaults()
	if dev {
		c = DevDefaults()
	}
	if path != "" {
		if _, err := toml.DecodeFile(path, &c); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	if err := applyEnv(&c, os.LookupEnv); err != nil {
		return Config{}, err
	}
	c.Dev = dev
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// applyEnv takes a lookup function (instead of calling os.LookupEnv directly)
// so tests can pass a fake environment.
func applyEnv(c *Config, lookup func(string) (string, bool)) error {
	str := map[string]*string{
		"LAYTE_LISTEN":          &c.Listen,
		"LAYTE_DATA_DIR":        &c.DataDir,
		"LAYTE_AGENT_SOCKET":    &c.AgentSocket,
		"LAYTE_SECRET_KEY_FILE": &c.SecretKeyFile,
		"LAYTE_DEV_ORIGIN":      &c.DevOrigin,
	}
	for k, p := range str {
		if v, ok := lookup(k); ok {
			*p = v
		}
	}
	if v, ok := lookup("LAYTE_TRUSTED_PROXIES"); ok {
		c.TrustedProxies = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.TrustedProxies = append(c.TrustedProxies, s)
			}
		}
	}
	durs := map[string]*Duration{
		"LAYTE_SESSION_IDLE": &c.SessionIdle,
		"LAYTE_SESSION_MAX":  &c.SessionMax,
	}
	for k, p := range durs {
		if v, ok := lookup(k); ok {
			if err := p.UnmarshalText([]byte(v)); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	return nil
}

// Validate checks the values make sense.
func (c Config) Validate() error {
	if _, port, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen %q: %w", c.Listen, err)
	} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("listen %q: invalid port", c.Listen)
	}
	for _, p := range c.TrustedProxies {
		if net.ParseIP(p) == nil {
			return fmt.Errorf("trusted_proxies: %q is not an IP address", p)
		}
	}
	if c.DataDir == "" || c.AgentSocket == "" || c.SecretKeyFile == "" {
		return errors.New("data_dir, agent_socket and secret_key_file are required")
	}
	if c.SessionIdle.Duration <= 0 || c.SessionMax.Duration < c.SessionIdle.Duration {
		return errors.New("session_idle must be > 0 and <= session_max")
	}
	return nil
}

// DBPath is the SQLite file inside the data dir.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "layte.db") }
