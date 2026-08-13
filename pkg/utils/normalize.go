// Package utils provides small shared helpers used across layers.
package utils

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalizeName canonicalizes an entity name (artist name, album title, ...)
// for cross-source dedup comparison: NFKC canonicalization (full-width to
// half-width, compatibility ligatures), lowercasing, and removal of
// whitespace, punctuation, and symbols. "周杰伦" stays as-is; "The Beatles"
// becomes "thebeatles"; "ＣＨＥＣＫ　ＩＴ！" becomes "checkit".
//
// The result is stable across sources so that format-only differences merge
// while genuinely different names (e.g. "Jay Chou" vs "周杰伦") do not.
// A name consisting only of symbols (e.g. the band "!!!") would normalize to
// the empty string and collide with every other symbol-only name, so an
// empty result falls back to the lowercased, trimmed original input.
func NormalizeName(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	normalized := strings.TrimSpace(b.String())
	if normalized == "" {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return normalized
}
