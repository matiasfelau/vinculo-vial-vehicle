package symmetric

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func claveDePrueba(t *testing.T) string {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("no se pudo generar la clave de prueba: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	cipher, err := NewCipher(claveDePrueba(t))
	if err != nil {
		t.Fatalf("no se pudo construir el Cipher: %v", err)
	}

	plaintext := []byte("AA000AA")

	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	got, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("no se pudo descifrar: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("texto en claro esperado %q, obtuvo %q", plaintext, got)
	}
}

func TestEncryptNoEsDeterministico(t *testing.T) {
	cipher, err := NewCipher(claveDePrueba(t))
	if err != nil {
		t.Fatalf("no se pudo construir el Cipher: %v", err)
	}

	plaintext := []byte("AA000AA")

	a, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}
	b, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	if a == b {
		t.Fatal("Encrypt produjo el mismo criptograma dos veces (nonce no aleatorio)")
	}
}

func TestNewCipherClaveInvalida(t *testing.T) {
	if _, err := NewCipher("no-es-base64!!"); err == nil {
		t.Fatal("esperaba error con clave con codificación base64 inválida")
	}
}

func TestNewCipherClaveDeLongitudIncorrecta(t *testing.T) {
	key := make([]byte, 16)
	if _, err := NewCipher(base64.StdEncoding.EncodeToString(key)); err == nil {
		t.Fatal("esperaba error con clave de 16 bytes (se requieren 32)")
	}
}

func TestDecryptCriptogramaAdulterado(t *testing.T) {
	cipher, err := NewCipher(claveDePrueba(t))
	if err != nil {
		t.Fatalf("no se pudo construir el Cipher: %v", err)
	}

	ciphertext, err := cipher.Encrypt([]byte("AA000AA"))
	if err != nil {
		t.Fatalf("no se pudo cifrar: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("no se pudo decodificar el criptograma: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF
	adulterado := base64.StdEncoding.EncodeToString(raw)

	if _, err := cipher.Decrypt(adulterado); err == nil {
		t.Fatal("esperaba error de autenticación con criptograma adulterado")
	}
}
