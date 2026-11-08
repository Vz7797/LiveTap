package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

type Vault struct{ aead cipher.AEAD }

func New(key []byte) (*Vault, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	return &Vault{a}, e
}
func (v *Vault) Seal(value []byte, purpose string) string {
	n := make([]byte, v.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		panic(e)
	}
	return base64.StdEncoding.EncodeToString(v.aead.Seal(n, n, value, []byte(purpose)))
}
func (v *Vault) Open(value, purpose string) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(value)
	if e != nil || len(b) < v.aead.NonceSize() {
		return nil, errors.New("加密数据无效")
	}
	out, e := v.aead.Open(nil, b[:v.aead.NonceSize()], b[v.aead.NonceSize():], []byte(purpose))
	if e != nil {
		return nil, errors.New("无法解密配置，请检查 LIVETAP_SECRET_KEY")
	}
	return out, nil
}
