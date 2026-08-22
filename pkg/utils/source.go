package utils

import "strings"

// DefaultSource is the fallback metadata source namespace when no other
// source is available. The user cache is always accessible, making it the
// safe default for new records that lack a platform source.
const DefaultSource = "user"

// NormalizeSource canonicalizes a client-supplied metadata source value
// (trimmed, lowercased) before it is persisted or compared.
func NormalizeSource(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
