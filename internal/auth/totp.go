package auth

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// VerifyTOTP validates a 6-digit TOTP code against a base32-encoded secret
// using the standard 30-second period and a ±1 step skew. The underlying
// pquerna/otp library performs constant-time comparison of the generated code,
// mitigating timing attacks.
func VerifyTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}

// VerifyTOTPWithSkew validates a TOTP code allowing a custom number of 30-second
// time steps around the current time. Use it to tolerate larger clock drift.
func VerifyTOTPWithSkew(secret, code string, skew uint) bool {
	ok, _ := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      skew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return ok
}
