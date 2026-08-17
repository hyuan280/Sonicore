package secrets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustNew(t *testing.T, master []byte) *Encryptor {
	t.Helper()
	e, err := New(master)
	require.NoError(t, err)
	return e
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	e := mustNew(t, []byte("0123456789abcdef0123456789abcdef"))
	for _, s := range []string{"MUSIC_U=abc; MUSIC_A=def", "中文凭据", "a:b:c"} {
		enc, err := e.Encrypt(s)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(enc, storedPrefix), "encrypted value marked with prefix")
		assert.NotEqual(t, s, enc, "encrypted value never equals plaintext")
		dec, err := e.Decrypt(enc)
		require.NoError(t, err)
		assert.Equal(t, s, dec)
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	e := mustNew(t, []byte("0123456789abcdef0123456789abcdef"))
	a, _ := e.Encrypt("same")
	b, _ := e.Encrypt("same")
	assert.NotEqual(t, a, b, "random nonce means two encryptions differ")
}

func TestEmptyStaysEmpty(t *testing.T) {
	e := mustNew(t, []byte("0123456789abcdef0123456789abcdef"))
	enc, err := e.Encrypt("")
	require.NoError(t, err)
	assert.Equal(t, "", enc)
	dec, err := e.Decrypt("")
	require.NoError(t, err)
	assert.Equal(t, "", dec)
}

func TestDecryptLegacyPlaintext(t *testing.T) {
	e := mustNew(t, []byte("0123456789abcdef0123456789abcdef"))
	// A value stored before encryption existed is returned as-is.
	dec, err := e.Decrypt("MUSIC_U=legacy")
	require.NoError(t, err)
	assert.Equal(t, "MUSIC_U=legacy", dec)
}

func TestDecryptWrongKeyFails(t *testing.T) {
	enc, err := mustNew(t, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")).Encrypt("secret")
	require.NoError(t, err)
	_, err = mustNew(t, []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")).Decrypt(enc)
	require.Error(t, err, "a different master secret must not decrypt the value")
}

func TestDecryptCorruptFails(t *testing.T) {
	e := mustNew(t, []byte("0123456789abcdef0123456789abcdef"))
	_, err := e.Decrypt(storedPrefix + "!!!not-base64!!!")
	require.Error(t, err)
	_, err = e.Decrypt(storedPrefix + "c2hvcnQ=") // decodes but too short
	require.Error(t, err)
}

func TestNewRejectsShortMaster(t *testing.T) {
	_, err := New([]byte("too-short"))
	require.Error(t, err, "a short/default master must not derive a predictable key")
}

func TestNewRejectsDefaultPlaceholder(t *testing.T) {
	_, err := New([]byte(defaultPlaceholder))
	require.Error(t, err, "the shipped config.example.toml placeholder is publicly known and must be rejected")
}
