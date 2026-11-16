package blindindex

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestDedupKeyCoincideConElHMACDelContrato(t *testing.T) {
	key := []byte("clave-dedup-de-prueba")

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("0b0e1f0e-6f7a-4b9e-8f43-2d6a5c1e9a10:AB123CD"))
	want := hex.EncodeToString(mac.Sum(nil))

	got := DedupKey(key, "0b0e1f0e-6f7a-4b9e-8f43-2d6a5c1e9a10", []byte("AB123CD"))

	if got != want {
		t.Fatalf("DedupKey = %q, el contrato pide HMAC-SHA256(key, issuer_id:patente) = %q", got, want)
	}
	if len(got) != 64 {
		t.Fatalf("la clave debe ser un hex de 64 caracteres, tiene %d", len(got))
	}
}

func TestDedupKeyEsDeterministica(t *testing.T) {
	key := []byte("clave-dedup-de-prueba")

	a := DedupKey(key, "emisor-1", []byte("AB123CD"))
	b := DedupKey(key, "emisor-1", []byte("AB123CD"))

	if a != b {
		t.Fatalf("DedupKey no es determinística: %q != %q", a, b)
	}
}

func TestDedupKeyNoRelacionaEmisoresDistintosAUnaMismaPatente(t *testing.T) {
	key := []byte("clave-dedup-de-prueba")

	a := DedupKey(key, "emisor-1", []byte("AB123CD"))
	b := DedupKey(key, "emisor-2", []byte("AB123CD"))

	if a == b {
		t.Fatal("dos emisores a la misma patente obtuvieron la misma clave")
	}
}

func TestDedupKeyDifiereConPatenteOClaveDistinta(t *testing.T) {
	base := DedupKey([]byte("clave-uno"), "emisor-1", []byte("AB123CD"))

	if base == DedupKey([]byte("clave-uno"), "emisor-1", []byte("AA000AA")) {
		t.Fatal("patentes distintas produjeron la misma clave")
	}
	if base == DedupKey([]byte("clave-dos"), "emisor-1", []byte("AB123CD")) {
		t.Fatal("claves HMAC distintas produjeron la misma clave de dedup")
	}
}

func TestDedupKeyDifiereDelBlindIndex(t *testing.T) {
	secret := []byte("mismo-secreto")

	if DedupKey(secret, "emisor-1", []byte("AB123CD")) == Compute(secret, []byte("AB123CD")) {
		t.Fatal("la clave de dedup no puede coincidir con el Blind Index de la patente")
	}
}
