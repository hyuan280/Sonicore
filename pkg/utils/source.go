package utils

import "strings"

// SourceMusicBrainz is the canonical name of the legacy metadata source.
// Every pre-existing record and every path that predates multi-source
// metadata lives under it.
const SourceMusicBrainz = "musicbrainz"

// SourceOrDefault maps an empty metadata source to the legacy default so
// callers that predate multi-source metadata keep working.
func SourceOrDefault(s string) string {
	if s == "" {
		return SourceMusicBrainz
	}
	return s
}

// NormalizeSource canonicalizes a client-supplied metadata source value
// (trimmed, lowercased) before it is persisted or compared.
func NormalizeSource(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
