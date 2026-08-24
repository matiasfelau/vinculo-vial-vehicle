// Package hpke desencripta criptogramas dirigidos a este servicio usando el
// esquema HPKE X25519/HKDF-SHA256/AES-256-GCM (RFC 9180) en modo base.
package hpke

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"
)

const (
	kemID  = hpke.KEM_X25519_HKDF_SHA256
	kdfID  = hpke.KDF_HKDF_SHA256
	aeadID = hpke.AEAD_AES256GCM
)

// Decryptor desencripta criptogramas HPKE cifrados con la clave pública de este servicio.
//
// El criptograma transportado (payload_enc) es la concatenación enc || ciphertext,
// donde enc es la clave encapsulada por el KEM y ciphertext es el sellado AEAD,
// codificada en base64 estándar o URL-safe (ver decodeCiphertextBase64). No se usa
// info ni AAD (ambos vacíos).
type Decryptor struct {
	privateKey kem.PrivateKey
}

// NewDecryptor construye un Decryptor a partir de la clave privada HPKE (X25519)
// codificada en base64 estándar.
func NewDecryptor(privateKeyBase64 string) (*Decryptor, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("hpke: clave privada con codificación base64 inválida: %w", err)
	}

	privateKey, err := kemID.Scheme().UnmarshalBinaryPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("hpke: clave privada inválida: %w", err)
	}

	return &Decryptor{privateKey: privateKey}, nil
}

// Decrypt desencripta el criptograma recibido y retorna el texto en claro como []byte.
//
// El criptograma proviene de clientes externos (apps móviles) cuyas librerías nativas
// a veces agregan espacios/saltos de línea o usan base64 URL-safe en lugar de estándar;
// decodeCiphertextBase64 tolera ambas variantes.
func (d *Decryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	raw, err := decodeCiphertextBase64(ciphertextBase64)
	if err != nil {
		return nil, fmt.Errorf("hpke: criptograma con codificación base64 inválida: %w", err)
	}

	encSize := kemID.Scheme().CiphertextSize()
	if len(raw) <= encSize {
		return nil, fmt.Errorf("hpke: criptograma demasiado corto")
	}
	enc, sealed := raw[:encSize], raw[encSize:]

	suite := hpke.NewSuite(kemID, kdfID, aeadID)
	receiver, err := suite.NewReceiver(d.privateKey, nil)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo inicializar el receptor: %w", err)
	}

	opener, err := receiver.Setup(enc)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo completar el establecimiento HPKE: %w", err)
	}

	plaintext, err := opener.Open(sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo desencriptar el criptograma: %w", err)
	}

	return plaintext, nil
}

// decodeCiphertextBase64 decodifica un criptograma recibido de un cliente externo,
// tolerando espacios/saltos de línea y tanto base64 estándar como URL-safe: algunas
// librerías nativas móviles introducen estas discrepancias de formato.
func decodeCiphertextBase64(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)

	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err == nil {
		return raw, nil
	}

	if raw, urlErr := base64.URLEncoding.DecodeString(trimmed); urlErr == nil {
		return raw, nil
	}

	return nil, err
}
