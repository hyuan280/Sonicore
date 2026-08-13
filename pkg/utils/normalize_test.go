package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"周杰伦", "周杰伦"},
		{"The Beatles", "thebeatles"},
		{"  The   Beatles!  ", "thebeatles"},
		{"ＣＨＥＣＫ　ＩＴ！", "checkit"}, // full-width to half-width
		{"A.B&C (Live)", "abclive"}, // punctuation stripped, inner space dropped
		{"中　文　名", "中文名"},      // full-width spaces dropped
		{"", ""},
		{"JAY CHOU", "jaychou"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, NormalizeName(c.in), "input %q", c.in)
	}
}

func TestNormalizeNameStableAcrossFormatting(t *testing.T) {
	// Format-only differences must merge to the same key.
	variants := []string{"Taylor Swift", "taylor swift", "Ｔａｙｌｏｒ　Ｓｗｉｆｔ", "Taylor-Swift", "TAYLOR SWIFT"}
	key := NormalizeName(variants[0])
	for _, v := range variants[1:] {
		assert.Equal(t, key, NormalizeName(v), "variant %q", v)
	}
}

func TestNormalizeNameKeepsDistinctNamesDistinct(t *testing.T) {
	// Genuinely different writing systems must NOT collide.
	assert.NotEqual(t, NormalizeName("Jay Chou"), NormalizeName("周杰伦"))
}

func TestNormalizeNameSymbolOnlyFallback(t *testing.T) {
	// Symbol-only names must not normalize to the empty dedup key; they keep
	// their (lowercased) original form so distinct inputs never collide.
	assert.Equal(t, "!!!", NormalizeName("!!!"))
	assert.Equal(t, "???", NormalizeName("???"))
	assert.Equal(t, "!!!", NormalizeName(" !!! "))
	assert.NotEqual(t, NormalizeName("!!!"), NormalizeName("???"))
}
