package netease

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
)

const (
	presetKey   = "0CoJUm6Qyw8W8jud"
	linuxapiKey = "rFgB&h#%2?^eDg:Q"
	eapiKey     = "e82ckenh8dichen8"
	aesIV       = "0102030405060708"
	base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	md5Magic    = "-36cd479b6b5-"
)

const rsaPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDgtQn2JZ34ZC28NWYpAUd98iZ37BUrX/aKzmFbt7clFSs6sXqHauqKWqdtLkF2KexO40H1YTX8z2lSgBBOAxLsvaklV8k4cBFK9snQXE9/DDaFt6Rr7iVZMldczhC0JNgTz+SHXT6CBHuX3e9SdB1Ua44oncaTWz7OBGLbCiK45wIDAQAB
-----END PUBLIC KEY-----`

// marshalJSON mirrors JS JSON.stringify: no HTML escaping, compact form.
func marshalJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padding := int(data[len(data)-1])
	if padding <= 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func aesECBEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plaintext = pkcs7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], plaintext[i:i+block.BlockSize()])
	}
	return out, nil
}

func aesECBDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], ciphertext[i:i+block.BlockSize()])
	}
	return pkcs7Unpad(out)
}

func aesCBCEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plaintext = pkcs7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plaintext)
	return out, nil
}

var rsaPublicKey *rsa.PublicKey

func init() {
	block, _ := pem.Decode([]byte(rsaPublicKeyPEM))
	if block == nil {
		panic("netease: failed to decode RSA public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic("netease: failed to parse RSA public key: " + err.Error())
	}
	rsaPublicKey = pub.(*rsa.PublicKey)
}

func randomSecretKey(n int) (string, error) {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", fmt.Errorf("netease: random read failed: %w", err)
		}
		sb.WriteByte(base62Chars[idx.Int64()])
	}
	return sb.String(), nil
}

// rsaEncryptRaw mirrors node-forge's "NONE" scheme: raw RSA without padding,
// m^e mod n, left-padded to the modulus size.
func rsaEncryptRaw(data []byte) ([]byte, error) {
	m := new(big.Int).SetBytes(data)
	if m.Cmp(rsaPublicKey.N) >= 0 {
		return nil, fmt.Errorf("message too large for modulus")
	}
	c := new(big.Int).Exp(m, big.NewInt(int64(rsaPublicKey.E)), rsaPublicKey.N)
	out := make([]byte, rsaPublicKey.Size())
	c.FillBytes(out)
	return out, nil
}

// weapi: double AES-CBC + RSA-encrypted reversed secret key. The second
// layer encrypts the base64 string of the first layer's ciphertext,
// matching the reference JS implementation (CryptoJS toString()).
func weapi(params map[string]any) (map[string]string, error) {
	text, err := marshalJSON(params)
	if err != nil {
		return nil, err
	}
	secretKey, err := randomSecretKey(16)
	if err != nil {
		return nil, err
	}

	first, err := aesCBCEncrypt([]byte(presetKey), []byte(aesIV), []byte(text))
	if err != nil {
		return nil, err
	}
	second, err := aesCBCEncrypt([]byte(secretKey), []byte(aesIV), []byte(base64.StdEncoding.EncodeToString(first)))
	if err != nil {
		return nil, err
	}

	reversed := []byte(secretKey)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	encSecKey, err := rsaEncryptRaw(reversed)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"params":    base64.StdEncoding.EncodeToString(second),
		"encSecKey": hex.EncodeToString(encSecKey),
	}, nil
}

// eapi: AES-ECB over "<url>-36cd479b6b5-<json>-36cd479b6b5-<md5>".
func eapi(url string, params map[string]any) (map[string]string, error) {
	text, err := marshalJSON(params)
	if err != nil {
		return nil, err
	}
	digest := md5.Sum([]byte("nobody" + url + "use" + text + "md5forencrypt"))
	data := url + md5Magic + text + md5Magic + hex.EncodeToString(digest[:])

	enc, err := aesECBEncrypt([]byte(eapiKey), []byte(data))
	if err != nil {
		return nil, err
	}
	return map[string]string{"params": strings.ToUpper(hex.EncodeToString(enc))}, nil
}

// linuxapi: AES-ECB over the JSON body.
func linuxapi(params map[string]any) (map[string]string, error) {
	text, err := marshalJSON(params)
	if err != nil {
		return nil, err
	}
	enc, err := aesECBEncrypt([]byte(linuxapiKey), []byte(text))
	if err != nil {
		return nil, err
	}
	return map[string]string{"eparams": strings.ToUpper(hex.EncodeToString(enc))}, nil
}

func cloudmusicEncodeID(id string) string {
	const xorKey = "3go8&$8*3*3h0k(2)2"
	xored := make([]byte, len(id))
	for i := 0; i < len(id); i++ {
		xored[i] = id[i] ^ xorKey[i%len(xorKey)]
	}
	sum := md5.Sum(xored)
	return base64.StdEncoding.EncodeToString(sum[:])
}
