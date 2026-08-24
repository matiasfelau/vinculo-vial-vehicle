package hpke

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"
)

// Encryptor cifra datos en claro con la clave pública HPKE de un destinatario remoto
// (por ejemplo, la clave pública de Identity), en modo base, sin info ni AAD.
type Encryptor struct {
	publicKey kem.PublicKey
}

// NewEncryptor construye un Encryptor a partir de una clave pública HPKE (X25519)
// codificada en base64 estándar.
func NewEncryptor(publicKeyBase64 string) (*Encryptor, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("hpke: clave pública con codificación base64 inválida: %w", err)
	}

	publicKey, err := kemID.Scheme().UnmarshalBinaryPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("hpke: clave pública inválida: %w", err)
	}

	return &Encryptor{publicKey: publicKey}, nil
}

// Encrypt cifra el texto en claro y retorna la concatenación enc || ciphertext,
// donde enc es la clave encapsulada por el KEM y ciphertext es el sellado AEAD.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	suite := hpke.NewSuite(kemID, kdfID, aeadID)
	sender, err := suite.NewSender(e.publicKey, nil)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo inicializar el emisor: %w", err)
	}

	enc, sealer, err := sender.Setup(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo completar el establecimiento HPKE: %w", err)
	}

	sealed, err := sealer.Seal(plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("hpke: no se pudo sellar el texto en claro: %w", err)
	}

	return append(enc, sealed...), nil
}
