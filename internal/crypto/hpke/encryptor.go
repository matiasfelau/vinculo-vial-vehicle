package hpke

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"
)

// Encryptor cifra datos en claro con la clave pública HPKE de un destinatario remoto
// (por ejemplo, la clave pública de Identity o la de IA), en modo base y sin AAD.
// El parámetro info del contexto HPKE es opcional: lo fija cada llamada.
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

// Encrypt cifra el texto en claro sin info y retorna la concatenación enc || ciphertext.
// Equivale a EncryptWithInfo(plaintext, nil).
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	return e.EncryptWithInfo(plaintext, nil)
}

// EncryptWithInfo cifra el texto en claro atando el criptograma al info recibido
// (por ejemplo, el alert_id) y retorna la concatenación enc || ciphertext, donde enc
// es la clave encapsulada por el KEM y ciphertext es el sellado AEAD. El destinatario
// solo puede descifrarlo con el mismo info.
func (e *Encryptor) EncryptWithInfo(plaintext, info []byte) ([]byte, error) {
	suite := hpke.NewSuite(kemID, kdfID, aeadID)
	sender, err := suite.NewSender(e.publicKey, info)
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
