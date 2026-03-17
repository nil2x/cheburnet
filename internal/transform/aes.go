package transform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var errInvalidKey = errors.New("key must be 32 bytes length")

// GenerateSecret generates secret suitable for SecretToKey.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)

	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// SecretToKey transforms secret into key suitable for Encrypt.
//
// secret is expected to be received from GenerateSecret or be 64 characters hex string.
func SecretToKey(secret string) ([]byte, error) {
	key, err := hex.DecodeString(secret)

	if err != nil {
		return nil, err
	}

	if len(key) != 32 {
		return nil, errInvalidKey
	}

	return key, nil
}

// Encrypt encrypts the data. Use Decrypt with the same key to decrypt it.
//
// key is expected to be received from SecretToKey or be 32 bytes length.
func Encrypt(data []byte, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errInvalidKey
	}

	block, err := aes.NewCipher(key)

	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)

	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())

	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	return ciphertext, nil
}

// Decrypt decrypts the data that was encrypted using Encrypt.
//
// key must be the same that was used for Encrypt.
func Decrypt(data []byte, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errInvalidKey
	}

	block, err := aes.NewCipher(key)

	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)

	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()

	if len(data) < nonceSize {
		return nil, errors.New("data is malformed")
	}

	nonce, data := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)

	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
