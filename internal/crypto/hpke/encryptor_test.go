package hpke

import (
	"encoding/base64"
	"testing"

	"github.com/cloudflare/circl/hpke"
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

// openWithInfo descifra enc || ciphertext con el info indicado, usando circl
// directamente: el Decryptor del servicio solo soporta info vacío.
func openWithInfo(t *testing.T, privateKeyRaw, ciphertext, info []byte) ([]byte, error) {
	t.Helper()

	privateKey, err := kemID.Scheme().UnmarshalBinaryPrivateKey(privateKeyRaw)
	if err != nil {
		t.Fatalf("no se pudo leer la clave privada: %v", err)
	}

	encSize := kemID.Scheme().CiphertextSize()
	enc, sealed := ciphertext[:encSize], ciphertext[encSize:]

	receiver, err := hpke.NewSuite(kemID, kdfID, aeadID).NewReceiver(privateKey, info)
	if err != nil {
		t.Fatalf("no se pudo inicializar el receptor: %v", err)
	}
	opener, err := receiver.Setup(enc)
	if err != nil {
		t.Fatalf("no se pudo completar el establecimiento HPKE: %v", err)
	}

	return opener.Open(sealed, nil)
}

func newKeyPairBase64(t *testing.T) (publicKeyBase64 string, privateKeyRaw []byte) {
	t.Helper()

	publicKey, privateKey, err := kemID.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}
	publicKeyRaw, err := publicKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave pública: %v", err)
	}
	privateKeyRaw, err = privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}

	return base64.StdEncoding.EncodeToString(publicKeyRaw), privateKeyRaw
}

func TestEncryptWithInfoRoundTrip(t *testing.T) {
	publicKeyBase64, privateKeyRaw := newKeyPairBase64(t)
	encryptor, err := NewEncryptor(publicKeyBase64)
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}

	info := []byte("3f2b8c1e-0000-4000-8000-000000000001")
	plaintext := []byte("AB123CD")

	ciphertext, err := encryptor.EncryptWithInfo(plaintext, info)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	got, err := openWithInfo(t, privateKeyRaw, ciphertext, info)
	if err != nil {
		t.Fatalf("no se pudo descifrar con el mismo info: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("texto en claro esperado %q, obtuvo %q", plaintext, got)
	}
}

func TestEncryptWithInfoDistintoInfoFalla(t *testing.T) {
	publicKeyBase64, privateKeyRaw := newKeyPairBase64(t)
	encryptor, err := NewEncryptor(publicKeyBase64)
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}

	ciphertext, err := encryptor.EncryptWithInfo([]byte("AB123CD"), []byte("alert-a"))
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	if _, err := openWithInfo(t, privateKeyRaw, ciphertext, []byte("alert-b")); err == nil {
		t.Fatal("esperaba error al descifrar con un info distinto")
	}
	if _, err := openWithInfo(t, privateKeyRaw, ciphertext, nil); err == nil {
		t.Fatal("esperaba error al descifrar sin info un criptograma atado a info")
	}
}

// Regresión del flujo del DNI: Encrypt delega en EncryptWithInfo con info nil y su
// salida sigue siendo descifrable por el Decryptor de la contraparte (info vacío).
func TestEncryptDelegaConInfoNil(t *testing.T) {
	publicKeyBase64, privateKeyRaw := newKeyPairBase64(t)
	encryptor, err := NewEncryptor(publicKeyBase64)
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}
	decryptor, err := NewDecryptor(base64.StdEncoding.EncodeToString(privateKeyRaw))
	if err != nil {
		t.Fatalf("no se pudo construir el Decryptor: %v", err)
	}

	plaintext := []byte("30123456")
	ciphertext, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	got, err := decryptor.Decrypt(base64.StdEncoding.EncodeToString(ciphertext))
	if err != nil {
		t.Fatalf("el Decryptor no pudo descifrar la salida de Encrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("texto en claro esperado %q, obtuvo %q", plaintext, got)
	}

	if _, err := openWithInfo(t, privateKeyRaw, ciphertext, nil); err != nil {
		t.Fatalf("Encrypt debía quedar atado a info nil: %v", err)
	}
}
