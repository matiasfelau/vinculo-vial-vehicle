// Package symmetric cifra y descifra la patente en reposo con AES-256-GCM.
package symmetric

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher implementa el cifrado at-rest de la patente con AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher construye un Cipher a partir de una clave AES-256 (32 bytes)
// codificada en base64 estándar.
func NewCipher(keyBase64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("symmetric: clave con codificación base64 inválida: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("symmetric: la clave debe tener 32 bytes, tiene %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("symmetric: no se pudo inicializar AES: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("symmetric: no se pudo inicializar GCM: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt cifra el texto en claro y retorna nonce || ciphertext codificado en base64 estándar.
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("symmetric: no se pudo generar el nonce: %w", err)
	}

	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt desencripta un valor generado por Encrypt.
func (c *Cipher) Decrypt(ciphertextBase64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, fmt.Errorf("symmetric: criptograma con codificación base64 inválida: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return nil, errors.New("symmetric: criptograma demasiado corto")
	}

	nonce, sealed := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("symmetric: no se pudo desencriptar el criptograma: %w", err)
	}

	return plaintext, nil
}
