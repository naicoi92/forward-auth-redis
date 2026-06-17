package randutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Hex returns a hex-encoded string of n random bytes.
func Hex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random read: %w", err)
	}
	return hex.EncodeToString(b), nil
}
