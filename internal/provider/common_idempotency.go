package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// reservedPrefix is pmon's namespace for shipped policies, roles, groups, and tags. Writes to a
// name under it are refused server-side; catching it in the plan turns a mid-apply failure into a
// validation error naming the attribute.
const reservedPrefix = "system:"

// idempotencyKey derives a stable key from what a write is trying to achieve. pmon records the
// key and replays the original outcome for a repeat, so a retry after a dropped connection cannot
// create a second row or apply an update twice. The key must therefore depend on the intended
// end state and nothing else -- no clock, no counter -- or a retry would look like a new request.
func idempotencyKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// isReservedName reports whether name sits in pmon's shipped namespace.
func isReservedName(name string) bool {
	return strings.HasPrefix(name, reservedPrefix)
}
