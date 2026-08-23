package hpke

import (
	"encoding/base64"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	publicKey, privateKey, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}

	publicKeyRaw, err := publicKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave pública: %v", err)
	}
	privateKeyRaw, err := privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}

	encryptor, err := NewEncryptor(base64.StdEncoding.EncodeToString(publicKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}
	decryptor, err := NewDecryptor(base64.StdEncoding.EncodeToString(privateKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Decryptor: %v", err)
	}

	plaintext := []byte("12345678")

	ciphertext, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	got, err := decryptor.Decrypt(base64.StdEncoding.EncodeToString(ciphertext))
	if err != nil {
		t.Fatalf("no se pudo descifrar: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("texto en claro esperado %q, obtuvo %q", plaintext, got)
	}
}

func TestEncryptNoEsDeterministico(t *testing.T) {
	publicKey, _, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}
	publicKeyRaw, err := publicKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave pública: %v", err)
	}

	encryptor, err := NewEncryptor(base64.StdEncoding.EncodeToString(publicKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}

	plaintext := []byte("12345678")

	a, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}
	b, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	if string(a) == string(b) {
		t.Fatal("Encrypt produjo el mismo criptograma dos veces")
	}
}

func TestNewEncryptorClavePublicaInvalida(t *testing.T) {
	if _, err := NewEncryptor("no-es-base64-válido!!"); err == nil {
		t.Fatal("esperaba error con clave pública mal codificada")
	}
}
