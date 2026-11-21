// Package auth holds the security primitives: password hashing, TOTP,
// encryption of secrets at rest, session tokens and rate limiting.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters: OWASP's minimum recommendation (19 MiB, 2 passes,
// 1 thread). Keeps a login under ~50 ms and fits a 1 GB VPS.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

var usernameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)

// ValidateUsername enforces ^[a-z][a-z0-9_-]{2,31}$.
func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return errors.New("username must be 3-32 chars: lowercase letters, digits, _ or -, starting with a letter")
	}
	return nil
}

// ValidatePassword enforces length limits (length beats complexity rules).
func ValidatePassword(p string) error {
	n := utf8.RuneCountInString(p)
	if n < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(p) > MaxPasswordLen {
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLen)
	}
	return nil
}

// HashPassword returns a PHC-format string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks password against a PHC string. The parameters stored
// in the hash are used, so old hashes keep working if defaults change.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, fmt.Errorf("parse argon2 params: %w", err)
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	// Constant-time compare so timing doesn't leak how many bytes matched.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when the username doesn't exist, so a login
// for an unknown user takes as long as one for a real user.
// sync.OnceValue computes it on first use only (not at startup of every mode).
var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword("layte-dummy-password-for-timing")
	return h
})

// BurnTime spends the same CPU as a real password check.
func BurnTime(password string) { VerifyPassword(password, dummyHash()) }
