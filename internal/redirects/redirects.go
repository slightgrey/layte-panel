// Package redirects holds a site's simple redirects (a path on the site to
// another path or URL), which Layte renders into the site's nginx vhost as
// exact-match locations. Every value ends up in an nginx config, so the
// allowed characters are strict: nothing nginx would treat as syntax
// (spaces, quotes, ; { } \) or as a variable ($).
package redirects

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Redirect sends requests for From (an exact path, with or without a
// trailing slash) to To with status Code.
type Redirect struct {
	From string `json:"from"`
	To   string `json:"to"`
	Code int    `json:"code"` // 301 (permanent) or 302 (temporary)
}

// Max is how many redirects a site may have.
const Max = 200

var (
	// A path: letters, digits and - . _ ~ / only (no % encoding, no query).
	fromRe = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
	// A target path or URL: URL characters only; never $ (nginx variable),
	// quotes, spaces, ; { } \ or #.
	toRe = regexp.MustCompile(`^[A-Za-z0-9._~/:?&=%+,@!*()\[\]-]+$`)
)

// reserved are paths Layte's vhost handles itself.
var reserved = []string{"/wp-config.php"}

// Normalize trims spaces, defaults Code to 301, and drops a trailing slash
// from From ("/old/" and "/old" are the same redirect; both are matched).
func (r Redirect) Normalize() Redirect {
	r.From, r.To = strings.TrimSpace(r.From), strings.TrimSpace(r.To)
	if len(r.From) > 1 {
		r.From = strings.TrimRight(r.From, "/")
		if r.From == "" {
			r.From = "/"
		}
	}
	if r.Code == 0 {
		r.Code = 301
	}
	return r
}

// Validate checks one normalised redirect.
func (r Redirect) Validate() error {
	switch {
	case !fromRe.MatchString(r.From) || len(r.From) > 255:
		return fmt.Errorf("redirect from %q: use a path like /old-page (letters, digits, - . _ ~ /)", r.From)
	case r.From == "/":
		return fmt.Errorf("redirect from /: the home page can't be redirected here")
	case strings.Contains(r.From, "//") || slices.Contains(strings.Split(r.From, "/"), ".."):
		return fmt.Errorf("redirect from %q: invalid path", r.From)
	case slices.Contains(reserved, r.From) || strings.HasPrefix(r.From, "/.well-known/"):
		return fmt.Errorf("redirect from %q: Layte Panel handles this path itself", r.From)
	case r.Code != 301 && r.Code != 302:
		return fmt.Errorf("redirect from %s: status must be 301 or 302", r.From)
	case !toRe.MatchString(r.To) || len(r.To) > 2000:
		return fmt.Errorf("redirect from %s: target %q has characters that aren't allowed", r.From, r.To)
	}
	if strings.HasPrefix(r.To, "/") {
		if strings.HasPrefix(r.To, "//") {
			return fmt.Errorf("redirect from %s: target %q: use https://… for another site", r.From, r.To)
		}
		if strings.TrimRight(strings.SplitN(r.To, "?", 2)[0], "/") == r.From {
			return fmt.Errorf("redirect from %s: target is the same page (a redirect loop)", r.From)
		}
		return nil
	}
	u, err := url.Parse(r.To)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return fmt.Errorf("redirect from %s: target must be a path (/new-page) or an http(s):// URL", r.From)
	}
	return nil
}

// Clean normalises a list and checks it: each redirect, the count, and no
// path twice. It returns nil for an empty list.
func Clean(in []Redirect) ([]Redirect, error) {
	if len(in) > Max {
		return nil, fmt.Errorf("at most %d redirects per site", Max)
	}
	var out []Redirect
	seen := map[string]bool{}
	for _, r := range in {
		r = r.Normalize()
		if err := r.Validate(); err != nil {
			return nil, err
		}
		if seen[r.From] {
			return nil, fmt.Errorf("redirect from %s is listed twice", r.From)
		}
		seen[r.From] = true
		out = append(out, r)
	}
	return out, nil
}

// IsClean reports whether a list is already normalised and valid (the agent
// accepts only that).
func IsClean(in []Redirect) error {
	c, err := Clean(in)
	if err != nil {
		return err
	}
	if !slices.Equal(c, in) && len(in) > 0 {
		return fmt.Errorf("redirects must be in normalised form")
	}
	return nil
}

// Paths are the exact locations nginx matches for the redirect: the path,
// and the same with a trailing slash.
func (r Redirect) Paths() []string { return []string{r.From, r.From + "/"} }
