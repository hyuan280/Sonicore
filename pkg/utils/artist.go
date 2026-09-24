package utils

import "strings"

// ArtistListSeparator is the ASCII Unit Separator (U+001F) used to encode a
// list of artist names into a single VARCHAR value. It is a non-printable
// control character that cannot occur in a real artist name, so names that
// themselves contain a comma (e.g. the credit style "Last, First") round-trip
// losslessly — unlike the legacy comma-joined encoding.
const ArtistListSeparator = "\x1f"

// JoinArtistNames encodes artist names for storage. Blank names are dropped;
// an empty input yields "".
func JoinArtistNames(names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ArtistListSeparator)
}

// SplitArtistNames decodes a stored artist value produced by JoinArtistNames.
// Blank entries are dropped.
func SplitArtistNames(s string) []string {
	parts := strings.Split(s, ArtistListSeparator)
	out := make([]string, 0, len(parts))
	for _, name := range parts {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}
