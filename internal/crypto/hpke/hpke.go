// Package hpke desencripta criptogramas dirigidos a este servicio usando el
// esquema HPKE X25519/HKDF-SHA256/AES-256-GCM (RFC 9180) en modo base.
package hpke

import (
	"encoding/base64"
	"fmt"

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
// El criptograma transportado (pat_enc) se codifica en base64 estándar y es la
// concatenación enc || ciphertext, donde enc es la clave encapsulada por el KEM y
// ciphertext es el sellado AEAD. No se usa info ni AAD (ambos vacíos).
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
func (d *Decryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextBase64)
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
