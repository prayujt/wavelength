package accounts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// The associated data is retained so existing encrypted credentials remain readable.
func (s *Service) seal(password string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(box.Seal(nonce, nonce, []byte(password), []byte("wavelength/subsonic/v1"))), nil
}
func (s *Service) unseal(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < box.NonceSize() {
		return "", errors.New("invalid encrypted password")
	}
	body, err := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], []byte("wavelength/subsonic/v1"))
	return string(body), err
}
