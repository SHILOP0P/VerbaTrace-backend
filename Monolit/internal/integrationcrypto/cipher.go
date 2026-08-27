package integrationcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrUnavailable = errors.New("integration encryption unavailable")

type Cipher struct {
	aead    cipher.AEAD
	version int
}

func NewFromBase64(raw string, version int) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 || version <= 0 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create integration cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create integration AEAD: %w", err)
	}
	return &Cipher{aead: aead, version: version}, nil
}

func (c *Cipher) Version() int {
	if c == nil {
		return 0
	}
	return c.version
}

func (c *Cipher) Encrypt(plaintext []byte, associatedData string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrUnavailable
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, []byte(associatedData)), nil
}

func (c *Cipher) Decrypt(ciphertext []byte, associatedData string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrUnavailable
	}
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	return c.aead.Open(nil, nonce, ciphertext[c.aead.NonceSize():], []byte(associatedData))
}
