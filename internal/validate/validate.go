// Package validate holds the input rules shared by the API and the agent.
// The agent re-checks everything, even if the API already did.
package validate

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/idna"

	"github.com/slightgrey/layte-panel/internal/auth"
)

var (
	labelRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	dbIdentRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	passwordRe = regexp.MustCompile(`^[A-Za-z0-9]{24,64}$`)
	phpRe      = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	titleRe    = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,100}$`)
)

// idnaProfile is the strict "lookup" profile browsers use for domain names.
var idnaProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.Transitional(false), idna.ValidateLabels(true))

// Domain normalises and validates a site domain, returning its ASCII form
// ("Bücher.example" → "xn--bcher-kva.example"). No IPs, no wildcards, no
// paths: it ends up in file names and nginx configs.
func Domain(s string) (string, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if s == "" {
		return "", errors.New("domain is required")
	}
	if strings.ContainsAny(s, `/\*:@ `) || strings.Contains(s, "..") {
		return "", fmt.Errorf("domain %q contains invalid characters", s)
	}
	if net.ParseIP(s) != nil {
		return "", errors.New("use a domain name, not an IP address")
	}
	ascii, err := idnaProfile.ToASCII(s)
	if err != nil {
		return "", fmt.Errorf("domain %q: %v", s, err)
	}
	ascii = strings.ToLower(ascii)
	if len(ascii) > 253 {
		return "", errors.New("domain is too long")
	}
	labels := strings.Split(ascii, ".")
	if len(labels) < 2 {
		return "", errors.New("domain needs at least one dot (e.g. example.com)")
	}
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return "", fmt.Errorf("domain %q: invalid label %q", s, l)
		}
	}
	if labelRe.MatchString(labels[len(labels)-1]) && isAllDigits(labels[len(labels)-1]) {
		return "", errors.New("the last part of a domain can't be all digits")
	}
	return ascii, nil
}

// Domains validates a list (aliases), rejecting duplicates and the primary.
func Domains(primary string, list []string) ([]string, error) {
	if len(list) > 20 {
		return nil, errors.New("at most 20 aliases")
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		d, err := Domain(s)
		if err != nil {
			return nil, err
		}
		if d == primary || slices.Contains(out, d) {
			return nil, fmt.Errorf("duplicate domain %s", d)
		}
		out = append(out, d)
	}
	return out, nil
}

// Username is a Linux username: ^[a-z][a-z0-9_-]{2,31}$.
func Username(s string) error { return auth.ValidateUsername(s) }

// reservedUsers can never be used as site users.
var reservedUsers = []string{"root", "www-data", "layte", "mysql", "nobody", "admin", "ubuntu", "daemon", "bin", "sys", "sync", "games", "man", "mail", "news", "backup", "list", "irc", "proxy", "syslog", "messagebus", "sshd"}

// SiteUsername is Username plus a list of names that belong to the system.
func SiteUsername(s string) error {
	if err := Username(s); err != nil {
		return err
	}
	if slices.Contains(reservedUsers, s) || strings.HasPrefix(s, "systemd") {
		return fmt.Errorf("%q is reserved for the system", s)
	}
	return nil
}

// DefaultUsername derives a site username from a domain:
// "www.my-shop.co.uk" → "myshopcouk". The caller checks it's free.
func DefaultUsername(domain string) string {
	d := strings.TrimPrefix(domain, "www.")
	var b strings.Builder
	for _, r := range d {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	u := b.String()
	if u == "" || u[0] < 'a' || u[0] > 'z' {
		u = "site" + u
	}
	if len(u) > 24 {
		u = u[:24]
	}
	for len(u) < 3 {
		u += "x"
	}
	return u
}

// DBIdent is a MySQL database or user name: ^[a-z][a-z0-9_]{0,31}$. Strict on
// purpose so names never need quoting or escaping in SQL.
func DBIdent(s string) error {
	if !dbIdentRe.MatchString(s) {
		return fmt.Errorf("%q: database names must be 1-32 chars of a-z, 0-9, _ starting with a letter", s)
	}
	return nil
}

// Password is a generated secret: 24-64 alphanumerics (safe inside SQL quotes).
func Password(s string) error {
	if !passwordRe.MatchString(s) {
		return errors.New("password must be 24-64 letters and digits")
	}
	return nil
}

// Email validates an email address.
func Email(s string) error {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 {
		return fmt.Errorf("%q is not a valid email address", s)
	}
	return nil
}

// PHPVersion checks v looks like "8.3" and is installed.
func PHPVersion(v string, installed []string) error {
	if !phpRe.MatchString(v) {
		return fmt.Errorf("invalid PHP version %q", v)
	}
	if !slices.Contains(installed, v) {
		return fmt.Errorf("PHP %s is not installed (installed: %s)", v, strings.Join(installed, ", "))
	}
	return nil
}

// Title is a short human label (e.g. a WordPress site title): no control chars.
func Title(s string) error {
	if !titleRe.MatchString(s) {
		return errors.New("title must be 1-100 characters without control characters")
	}
	return nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
