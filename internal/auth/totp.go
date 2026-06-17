package auth

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// VerifyTOTP validates a 6-digit TOTP code against a base32-encoded secret
// using the standard 30-second period and the given skew. The underlying
// pquerna/otp library performs constant-time comparison of the generated code,
// mitigating timing attacks. A skew of 1 allows the current period plus one
// step before and one step after (common default to tolerate clock drift
// and codes that expire while being typed).
func VerifyTOTP(secret, code string, skew uint) bool {
	ok, _ := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      skew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return ok
}
