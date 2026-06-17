// Package userx provides username validation shared by the seed CLI and the
// auth service. Restricting usernames to a safe character set prevents Redis
// key collisions and keeps monitoring/alerting predictable.
package userx

import (
	"fmt"
	"regexp"
)

const (
	maxUsernameLen = 32
	minUsernameLen = 1
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidateUsername checks that a username contains only safe characters and is
// within the allowed length range. It returns an error describing why the name
// is invalid.
func ValidateUsername(username string) error {
	if len(username) < minUsernameLen || len(username) > maxUsernameLen {
		return fmt.Errorf("username must be between %d and %d characters", minUsernameLen, maxUsernameLen)
	}
	if !usernameRegex.MatchString(username) {
		return fmt.Errorf("username must contain only letters, digits, '.', '-' or '_'")
	}
	return nil
}
