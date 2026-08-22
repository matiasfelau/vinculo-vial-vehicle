package hpke

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/cloudflare/circl/hpke"
)

func TestDecryptRoundTrip(t *testing.T) {
	publicKey, privateKey, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}

	privateKeyRaw, err := privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}
	privateKeyBase64 := base64.StdEncoding.EncodeToString(privateKeyRaw)

	plaintext := []byte("AA000AA")

	suite := hpke.NewSuite(kemID, kdfID, aeadID)
	sender, err := suite.NewSender(publicKey, nil)
	if err != nil {
		t.Fatalf("no se pudo inicializar el emisor: %v", err)
	}

	enc, sealer, err := sender.Setup(rand.Reader)
	if err != nil {
		t.Fatalf("no se pudo completar el establecimiento HPKE del emisor: %v", err)
	}

	sealed, err := sealer.Seal(plaintext, nil)
	if err != nil {
		t.Fatalf("no se pudo sellar el texto en claro: %v", err)
	}

	ciphertextBase64 := base64.StdEncoding.EncodeToString(append(enc, sealed...))

	decryptor, err := NewDecryptor(privateKeyBase64)
	if err != nil {
		t.Fatalf("no se pudo construir el Decryptor: %v", err)
	}

	got, err := decryptor.Decrypt(ciphertextBase64)
	if err != nil {
		t.Fatalf("no se pudo desencriptar el criptograma: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("texto en claro esperado %q, obtuvo %q", plaintext, got)
	}
}

func TestDecryptClaveInvalida(t *testing.T) {
	if _, err := NewDecryptor("no-es-base64-válido!!"); err == nil {
		t.Fatal("esperaba error con clave privada mal codificada")
	}
}

func TestDecryptCriptogramaCorto(t *testing.T) {
	decryptor := decryptorConClaveDePrueba(t)

	if _, err := decryptor.Decrypt(base64.StdEncoding.EncodeToString([]byte("corto"))); err == nil {
		t.Fatal("esperaba error con criptograma demasiado corto")
	}
}

func TestDecryptCriptogramaAdulterado(t *testing.T) {
	publicKey, privateKey, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}

	privateKeyRaw, err := privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}

	suite := hpke.NewSuite(kemID, kdfID, aeadID)
	sender, err := suite.NewSender(publicKey, nil)
	if err != nil {
		t.Fatalf("no se pudo inicializar el emisor: %v", err)
	}

	enc, sealer, err := sender.Setup(rand.Reader)
	if err != nil {
		t.Fatalf("no se pudo completar el establecimiento HPKE del emisor: %v", err)
	}

	sealed, err := sealer.Seal([]byte("AA000AA"), nil)
	if err != nil {
		t.Fatalf("no se pudo sellar el texto en claro: %v", err)
	}

	adulterado := append(enc, sealed...)
	adulterado[len(adulterado)-1] ^= 0xFF

	decryptor, err := NewDecryptor(base64.StdEncoding.EncodeToString(privateKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Decryptor: %v", err)
	}

	if _, err := decryptor.Decrypt(base64.StdEncoding.EncodeToString(adulterado)); err == nil {
		t.Fatal("esperaba error de autenticación con criptograma adulterado")
	}
}

func decryptorConClaveDePrueba(t *testing.T) *Decryptor {
	t.Helper()

	_, privateKey, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}

	privateKeyRaw, err := privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}

	decryptor, err := NewDecryptor(base64.StdEncoding.EncodeToString(privateKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Decryptor: %v", err)
	}

	return decryptor
}
