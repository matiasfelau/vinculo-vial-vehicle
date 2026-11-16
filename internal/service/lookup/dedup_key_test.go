package lookup

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

var (
	testPatSecret   = []byte("secreto")
	testDedupSecret = []byte("secreto-dedup")
)

const testIssuerID = "0b0e1f0e-6f7a-4b9e-8f43-2d6a5c1e9a10"

// freshPlateDecryptor entrega una copia nueva de la patente en cada llamada, como el
// descifrador real: el servicio purga el buffer que recibe, así que reutilizar el mismo
// slice entre dos llamadas le pasaría ceros.
type freshPlateDecryptor struct{ plate []byte }

func (f *freshPlateDecryptor) Decrypt(string) ([]byte, error) {
	return append([]byte(nil), f.plate...), nil
}

func newDedupService(repo *fakeRepository, plate []byte) *ResolveOwnersService {
	return NewResolveOwnersService(repo, &freshPlateDecryptor{plate: plate}, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, testPatSecret, testDedupSecret, testPendingTTL)
}

func TestDedupKeyEsElHMACDelEmisorYLaPatenteCanonica(t *testing.T) {
	service := newDedupService(&fakeRepository{}, []byte("ab 123 cd"))

	got, err := service.DedupKey("criptograma", testIssuerID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	// La patente se normaliza antes: "ab 123 cd" y "AB123CD" son la misma patente.
	if want := blindindex.DedupKey(testDedupSecret, testIssuerID, []byte("AB123CD")); got != want {
		t.Fatalf("dedup_key = %q, esperaba la del emisor y la patente canónica %q", got, want)
	}
}

func TestDedupKeyUsaSuPropiaClaveYNoLaDelBlindIndex(t *testing.T) {
	service := newDedupService(&fakeRepository{}, []byte("AB123CD"))

	got, err := service.DedupKey("criptograma", testIssuerID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if got == blindindex.DedupKey(testPatSecret, testIssuerID, []byte("AB123CD")) {
		t.Fatal("la dedup_key se calculó con PAT_HMAC_SECRET en vez de ALERT_DEDUP_HMAC_KEY")
	}
}

func TestDedupKeyEmisoresDistintosNoSeRelacionan(t *testing.T) {
	service := newDedupService(&fakeRepository{}, []byte("AB123CD"))

	a, errA := service.DedupKey("criptograma", "emisor-a")
	b, errB := service.DedupKey("criptograma", "emisor-b")

	if errA != nil || errB != nil {
		t.Fatalf("no se esperaban errores: %v, %v", errA, errB)
	}
	if a == b {
		t.Fatal("dos emisores a la misma patente obtuvieron la misma dedup_key")
	}
}

func TestDedupKeyNoTocaLaBaseNiReservaPendientes(t *testing.T) {
	repo := &fakeRepository{}
	service := newDedupService(repo, []byte("AB123CD"))

	if _, err := service.DedupKey("criptograma", testIssuerID); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if repo.patHashRecibido != "" {
		t.Fatal("DedupKey consultó la base: no debe reservar patentes pendientes ni revelar si hay dueños")
	}
}

func TestDedupKeyConPatenteInvalidaDevuelveErrInvalidPlate(t *testing.T) {
	service := newDedupService(&fakeRepository{}, []byte("no-es-patente"))

	got, err := service.DedupKey("criptograma", testIssuerID)

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba ErrInvalidPlate, obtuvo %v", err)
	}
	if got != "" {
		t.Fatalf("ante un error no debe devolver ninguna clave, obtuvo %q", got)
	}
}

func TestDedupKeyConCriptogramaInvalidoDevuelveErrInvalidCryptogram(t *testing.T) {
	service := NewResolveOwnersService(&fakeRepository{}, &fakePlateDecryptor{err: errors.New("no abre")}, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, testPatSecret, testDedupSecret, testPendingTTL)

	_, err := service.DedupKey("criptograma", testIssuerID)

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba ErrInvalidCryptogram, obtuvo %v", err)
	}
}

func TestDedupKeyPurgaLaPatenteDeMemoria(t *testing.T) {
	// Acá el descifrador comparte el slice con la prueba para poder verificar la purga.
	plate := []byte("AB123CD")
	service := NewResolveOwnersService(&fakeRepository{}, &fakePlateDecryptor{plate: plate}, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, testPatSecret, testDedupSecret, testPendingTTL)

	if _, err := service.DedupKey("criptograma", testIssuerID); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	assertWiped(t, "la patente", plate)
}

func TestResolveOwnersConIssuerIDDevuelveLaDedupKey(t *testing.T) {
	want := blindindex.DedupKey(testDedupSecret, testIssuerID, []byte("AB123CD"))
	pendingRef := uuid.New()
	casos := map[string]*fakeRepository{
		"con dueños": {owners: []uuid.UUID{uuid.New()}},
		"sin dueños": {pendingRef: &pendingRef},
	}

	for nombre, repo := range casos {
		t.Run(nombre, func(t *testing.T) {
			service := newDedupService(repo, []byte("AB123CD"))

			res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), testIssuerID)

			if err != nil {
				t.Fatalf("no se esperaba error: %v", err)
			}
			if res.DedupKey != want {
				t.Fatalf("dedup_key = %q, esperaba %q", res.DedupKey, want)
			}
		})
	}
}

func TestResolveOwnersSinIssuerIDNoDevuelveDedupKey(t *testing.T) {
	service := newDedupService(&fakeRepository{}, []byte("AB123CD"))

	res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if res.DedupKey != "" {
		t.Fatalf("sin issuer_id la respuesta queda como antes, obtuvo dedup_key %q", res.DedupKey)
	}
}

func TestResolveOwnersYDedupKeyCoincidenParaElMismoEmisorYPatente(t *testing.T) {
	// El intento (dedup-key) y el envío (resolve-owner) deben producir la misma clave:
	// si no, el cupo del intento no valdría para el envío.
	service := newDedupService(&fakeRepository{}, []byte("AB123CD"))

	res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), testIssuerID)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	direct, err := service.DedupKey("criptograma", testIssuerID)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if res.DedupKey != direct {
		t.Fatalf("resolve-owner dio %q y dedup-key %q para el mismo emisor y patente", res.DedupKey, direct)
	}
}
