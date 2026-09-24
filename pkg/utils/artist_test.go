package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJoinSplitArtistNamesRoundTrip(t *testing.T) {
	cases := [][]string{
		{"Last, First"},
		{"AC/DC", "The Beatles"},
		{"A、B", "C & D"},
		{"周杰伦", "Jay Chou"},
		{"Spaced", "Two"},
	}
	for _, names := range cases {
		got := SplitArtistNames(JoinArtistNames(names))
		assert.Equal(t, names, got, "round-trip %v", names)
	}
}

func TestJoinArtistNamesDropsBlanks(t *testing.T) {
	assert.Equal(t, "", JoinArtistNames(nil))
	assert.Equal(t, "", JoinArtistNames([]string{"", "  "}))
	assert.Equal(t, "A", JoinArtistNames([]string{"", " A ", ""}))
}

func TestSplitArtistNamesKeepsCommaInsideName(t *testing.T) {
	// A comma is ordinary data now, not a separator.
	assert.Equal(t, []string{"Last, First"}, SplitArtistNames(JoinArtistNames([]string{"Last, First"})))
	assert.Equal(t, []string{"A, B", "C"}, SplitArtistNames(JoinArtistNames([]string{"A, B", "C"})))
}

func TestSplitArtistNamesDropsBlanks(t *testing.T) {
	assert.Empty(t, SplitArtistNames(""))
	assert.Equal(t, []string{"A", "B"}, SplitArtistNames("A"+ArtistListSeparator+"B"))
	assert.Equal(t, []string{"A", "B"}, SplitArtistNames(ArtistListSeparator+"A"+ArtistListSeparator+ArtistListSeparator+"B"+ArtistListSeparator))
}
