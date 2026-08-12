package netease

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalJSONCompactNoEscape(t *testing.T) {
	// HTML chars must not be escaped (mirrors JS JSON.stringify)
	s, err := marshalJSON(map[string]any{"url": "a<b>&c", "n": 1})
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":"a<b>&c","n":1}`, s)
	assert.NotContains(t, s, "\\u003c")
	assert.NotContains(t, s, "\n")
}

func TestMarshalJSONTrailingNewlineStripped(t *testing.T) {
	s, err := marshalJSON([]string{"x"})
	require.NoError(t, err)
	assert.Equal(t, `["x"]`, s)
	assert.False(t, strings.HasSuffix(s, "\n"))
}

func TestPKCS7Pad(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		blockSize int
		want      []byte
	}{
		{"exact block adds full block", []byte("12345678"), 8, append([]byte("12345678"), 8, 8, 8, 8, 8, 8, 8, 8)},
		{"partial fills remainder", []byte("12345"), 8, []byte{'1', '2', '3', '4', '5', 3, 3, 3}},
		{"empty gets full block", []byte{}, 8, []byte{8, 8, 8, 8, 8, 8, 8, 8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pkcs7Pad(tt.data, tt.blockSize))
		})
	}
}

func TestPKCS7Unpad(t *testing.T) {
	got, err := pkcs7Unpad([]byte{'1', '2', '3', '4', '5', 3, 3, 3})
	require.NoError(t, err)
	assert.Equal(t, []byte("12345"), got)
}

func TestPKCS7UnpadErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"zero padding byte", []byte{'a', 0}},
		{"padding larger than block", []byte{'a', 17}},
		{"padding larger than data", []byte{5, 6}},
		{"inconsistent padding bytes", []byte{'a', 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pkcs7Unpad(tt.data)
			require.Error(t, err)
		})
	}
}

func TestPKCS7RoundTrip(t *testing.T) {
	inputs := [][]byte{
		[]byte(""),
		[]byte("hello"),
		[]byte("1234567890abcdef"),
		[]byte(strings.Repeat("x", 40)),
	}
	for _, in := range inputs {
		padded := pkcs7Pad(in, aes.BlockSize)
		unpadded, err := pkcs7Unpad(padded)
		require.NoError(t, err)
		assert.Equal(t, in, unpadded)
	}
}

func TestAESECBRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	plaintext := []byte(`{"hello":"world"}`)

	enc, err := aesECBEncrypt(key, plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, enc, "ciphertext must differ from plaintext")

	dec, err := aesECBDecrypt(key, enc)
	require.NoError(t, err)
	assert.Equal(t, plaintext, dec)
}

func TestAESECBDifferentKeys(t *testing.T) {
	enc1, _ := aesECBEncrypt([]byte("1111111111111111"), []byte("payload"))
	enc2, _ := aesECBEncrypt([]byte("2222222222222222"), []byte("payload"))
	assert.NotEqual(t, enc1, enc2)
}

func TestAESECBDecryptErrors(t *testing.T) {
	key := []byte("0123456789abcdef")

	_, err := aesECBDecrypt(key, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid ciphertext length")

	_, err = aesECBDecrypt(key, []byte("short"))
	require.Error(t, err)
}

func TestAESCBCEncryptBlocks(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte("0102030405060708")
	plaintext := []byte("hello world")

	out, err := aesCBCEncrypt(key, iv, plaintext)
	require.NoError(t, err)
	assert.Equal(t, 16, len(out), "output must be padded to a full block")
}

func TestRandomSecretKey(t *testing.T) {
	s, err := randomSecretKey(16)
	require.NoError(t, err)
	assert.Len(t, s, 16)
	for _, c := range s {
		assert.True(t, strings.ContainsRune(base62Chars, c))
	}

	s2, _ := randomSecretKey(16)
	assert.NotEqual(t, s, s2, "keys should be random")

	empty, err := randomSecretKey(0)
	require.NoError(t, err)
	assert.Equal(t, "", empty)
}

func TestRSARawEncryptShape(t *testing.T) {
	out, err := rsaEncryptRaw([]byte("secret-key-material"))
	require.NoError(t, err)
	assert.Len(t, out, 128, "output must be exactly the modulus size")

	// deterministic for same input
	out2, err := rsaEncryptRaw([]byte("secret-key-material"))
	require.NoError(t, err)
	assert.Equal(t, out, out2)
}

func TestRSARawEncryptTooLarge(t *testing.T) {
	// 128 bytes of 0xFF >= 1024-bit modulus
	_, err := rsaEncryptRaw(bytes.Repeat([]byte{0xFF}, 128))
	require.Error(t, err)
}

func TestWeapiStructure(t *testing.T) {
	res, err := weapi(map[string]any{"ids": "[1,2]", "level": "standard"})
	require.NoError(t, err)

	params, ok := res["params"]
	require.True(t, ok, "must contain params")
	raw, err := base64.StdEncoding.DecodeString(params)
	require.NoError(t, err)
	assert.Zero(t, len(raw)%16, "params must be full CBC blocks")

	encSecKey, ok := res["encSecKey"]
	require.True(t, ok, "must contain encSecKey")
	assert.Len(t, encSecKey, 256, "128-byte RSA output hex-encoded")
}

func TestEapiDeterministic(t *testing.T) {
	params := map[string]any{"id": "123", "type": 1}

	r1, err := eapi("api/v3/song/detail", params)
	require.NoError(t, err)
	r2, err := eapi("api/v3/song/detail", params)
	require.NoError(t, err)
	assert.Equal(t, r1, r2, "eapi has no randomness")

	assertUppercaseHexBlocks(t, r1["params"])
}

func assertUppercaseHexBlocks(t *testing.T, s string) {
	t.Helper()
	for _, c := range s {
		assert.True(t, (c >= 'A' && c <= 'F') || (c >= '0' && c <= '9'), "non-hex char %q", c)
	}
	raw, err := hex.DecodeString(s)
	require.NoError(t, err)
	assert.Zero(t, len(raw)%16, "ciphertext must be full AES blocks")
}

func TestLinuxapiDeterministic(t *testing.T) {
	params := map[string]any{"u": "user", "token": "tok"}

	r1, err := linuxapi(params)
	require.NoError(t, err)
	r2, err := linuxapi(params)
	require.NoError(t, err)
	assert.Equal(t, r1, r2)
	assertUppercaseHexBlocks(t, r1["eparams"])
}

func TestCloudmusicEncodeIDKnownVector(t *testing.T) {
	// Regression vectors captured from this implementation (matches reference JS).
	assert.Equal(t, "s802kfjPg7kbBzK5KmthwA==", cloudmusicEncodeID("13351914032993659"))
	assert.Equal(t, "dFkp/Nj1AGO5UmVgIqdC7g==", cloudmusicEncodeID("33894332"))
}

func TestCloudmusicEncodeIDDeterministic(t *testing.T) {
	a := cloudmusicEncodeID("track-1")
	b := cloudmusicEncodeID("track-1")
	c := cloudmusicEncodeID("track-2")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Equal(t, "track-1", "track-1")
	_, err := base64.StdEncoding.DecodeString(cloudmusicEncodeID("x"))
	require.NoError(t, err)
}
