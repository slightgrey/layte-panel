package auth

import (
	"crypto/subtle"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTP settings: the standard ones every authenticator app supports.
const (
	totpPeriod = 30
	totpSkew   = 1 // accept the previous and next 30s window for clock drift
	totpIssuer = "Layte Panel"
)

var totpOpts = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// TOTPEnrollment is a newly generated TOTP secret.
type TOTPEnrollment struct {
	Secret string // base32, what the user types if they can't scan
	URL    string // otpauth:// URI for QR codes
}

// NewTOTP generates a fresh secret for account (the username).
func NewTOTP(account string) (TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: account,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPEnrollment{}, err
	}
	return TOTPEnrollment{Secret: key.Secret(), URL: key.URL()}, nil
}

// CheckTOTP validates code against secret at time now. It returns the matched
// time step, which must be greater than lastStep: a code that was already used
// (or an older one) is rejected, so a sniffed code can't be replayed.
func CheckTOTP(secret, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	for skew := int64(-totpSkew); skew <= totpSkew; skew++ {
		s := current + skew
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totpOpts)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			if s <= lastStep {
				return 0, false
			}
			return s, true
		}
	}
	return 0, false
}

// TOTPCode generates the code for a time; used by tests and the CLI.
func TOTPCode(secret string, t time.Time) (string, error) {
	return totp.GenerateCodeCustom(secret, t, totpOpts)
}
