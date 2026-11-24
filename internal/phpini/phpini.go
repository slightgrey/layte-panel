// Package phpini holds the per-site PHP settings Layte Panel lets admins
// change (simple mode): a few php.ini values written into the site's PHP-FPM
// pool. Values are checked strictly, because they end up in config files.
package phpini

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Settings are a site's PHP settings. Zero values mean "Layte's default".
type Settings struct {
	MemoryLimit      string `json:"memory_limit,omitempty"`       // e.g. "256M"
	UploadMax        string `json:"upload_max,omitempty"`         // upload_max_filesize = post_max_size = nginx client_max_body_size
	MaxExecutionTime int    `json:"max_execution_time,omitempty"` // seconds
	MaxInputVars     int    `json:"max_input_vars,omitempty"`
}

// Defaults: what a site gets when nothing is set.
const (
	DefaultMemoryLimit      = "256M"
	DefaultUploadMax        = "64M"
	DefaultMaxExecutionTime = 30 // PHP's own default
	DefaultMaxInputVars     = 1000
)

// Limits for custom values.
const (
	minMemoryMB, maxMemoryMB = 32, 4096
	minUploadMB, maxUploadMB = 1, 2048
	minExec, maxExec         = 1, 3600
	minVars, maxVars         = 100, 100000
)

var sizeRe = regexp.MustCompile(`^([1-9][0-9]{0,4})([MG])$`)

// sizeMB parses "256M" or "1G" (only M and G, upper case, no spaces).
func sizeMB(v string) (int, error) {
	m := sizeRe.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("%q isn't a size like 256M or 1G", v)
	}
	n, _ := strconv.Atoi(m[1])
	if m[2] == "G" {
		n *= 1024
	}
	return n, nil
}

// Normalize trims spaces and upper-cases the unit ("512m" → "512M").
func (s Settings) Normalize() Settings {
	s.MemoryLimit = strings.ToUpper(strings.TrimSpace(s.MemoryLimit))
	s.UploadMax = strings.ToUpper(strings.TrimSpace(s.UploadMax))
	return s
}

// Validate checks every set value against its format and range.
func (s Settings) Validate() error {
	if s.MemoryLimit != "" {
		mb, err := sizeMB(s.MemoryLimit)
		if err != nil {
			return fmt.Errorf("memory limit: %w", err)
		}
		if mb < minMemoryMB || mb > maxMemoryMB {
			return fmt.Errorf("memory limit must be between %dM and %dG", minMemoryMB, maxMemoryMB/1024)
		}
	}
	if s.UploadMax != "" {
		mb, err := sizeMB(s.UploadMax)
		if err != nil {
			return fmt.Errorf("max upload size: %w", err)
		}
		if mb < minUploadMB || mb > maxUploadMB {
			return fmt.Errorf("max upload size must be between %dM and %dG", minUploadMB, maxUploadMB/1024)
		}
	}
	if s.MaxExecutionTime != 0 && (s.MaxExecutionTime < minExec || s.MaxExecutionTime > maxExec) {
		return fmt.Errorf("max execution time must be between %d and %d seconds", minExec, maxExec)
	}
	if s.MaxInputVars != 0 && (s.MaxInputVars < minVars || s.MaxInputVars > maxVars) {
		return fmt.Errorf("max input vars must be between %d and %d", minVars, maxVars)
	}
	return nil
}

// Memory is the effective memory_limit.
func (s Settings) Memory() string { return or(s.MemoryLimit, DefaultMemoryLimit) }

// Upload is the effective upload_max_filesize (and post_max_size).
func (s Settings) Upload() string { return or(s.UploadMax, DefaultUploadMax) }

// NginxBodySize is client_max_body_size for nginx ("64m").
func (s Settings) NginxBodySize() string { return strings.ToLower(s.Upload()) }

// FastCGIReadTimeout is how long nginx waits for PHP (seconds): 300, or a
// bit longer than max_execution_time, so nginx doesn't cut long scripts off.
func (s Settings) FastCGIReadTimeout() int { return max(300, s.MaxExecutionTime+30) }

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
