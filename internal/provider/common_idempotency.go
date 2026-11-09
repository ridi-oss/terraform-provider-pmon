package provider

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// reservedPrefix is pmon's namespace for shipped policies, roles, groups, and tags. Writes to a
// name under it are refused server-side; catching it in the plan turns a mid-apply failure into a
// validation error naming the attribute.
const reservedPrefix = "system:"

func newIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// isReservedName reports whether name sits in pmon's shipped namespace.
func isReservedName(name string) bool {
	return strings.HasPrefix(name, reservedPrefix)
}
