package lookup

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	circl "github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

const (
	testKEM  = circl.KEM_X25519_HKDF_SHA256
	testKDF  = circl.KDF_HKDF_SHA256
	testAEAD = circl.AEAD_AES256GCM
)

// fakePlateEncryptor registra lo que recibe y, opcionalmente, falla.
type fakePlateEncryptor struct {
	err          error
	invocado     bool
	plaintext    []byte
	infoRecibido []byte
}

func (f *fakePlateEncryptor) EncryptWithInfo(plaintext, info []byte) ([]byte, error) {
	f.invocado = true
	f.plaintext = plaintext
	f.infoRecibido = info
	if f.err != nil {
		return nil, f.err
	}
	return []byte("sellado"), nil
}

func newKeyPair(t *testing.T) (publicKeyBase64 string, privateKey kem.PrivateKey) {
	t.Helper()

	publicKey, privateKey, err := testKEM.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}
	raw, err := publicKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave pública: %v", err)
	}

	return base64.StdEncoding.EncodeToString(raw), privateKey
}

func newRealEncryptor(t *testing.T, publicKeyBase64 string) *hpke.Encryptor {
	t.Helper()

	encryptor, err := hpke.NewEncryptor(publicKeyBase64)
	if err != nil {
		t.Fatalf("no se pudo construir el Encryptor: %v", err)
	}
	return encryptor
}

// openWithInfo descifra el base64 de enc‖ct como lo haría IA.
func openWithInfo(privateKey kem.PrivateKey, ciphertextBase64 string, info []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, err
	}
	encSize := testKEM.Scheme().CiphertextSize()
	enc, sealed := raw[:encSize], raw[encSize:]

	receiver, err := circl.NewSuite(testKEM, testKDF, testAEAD).NewReceiver(privateKey, info)
	if err != nil {
		return nil, err
	}
	opener, err := receiver.Setup(enc)
	if err != nil {
		return nil, err
	}

	return opener.Open(sealed, nil)
}

func assertWiped(t *testing.T, nombre string, data []byte) {
	t.Helper()

	for i, b := range data {
		if b != 0 {
			t.Fatalf("byte %d de %s no fue purgado: %d", i, nombre, b)
		}
	}
}

const testPendingTTL = 24 * time.Hour

func TestResolveOwnersPatenteConTresDuenosRetornaLosTresUUIDs(t *testing.T) {
	owners := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	repo := &fakeRepository{owners: owners}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(res.OwnerIDs) != 3 {
		t.Fatalf("esperaba 3 dueños, obtuvo %d", len(res.OwnerIDs))
	}
	for i, ownerID := range owners {
		if res.OwnerIDs[i] != ownerID {
			t.Fatalf("owner_id en posición %d = %v, esperaba %v", i, res.OwnerIDs[i], ownerID)
		}
	}
	if res.PendingRef != nil {
		t.Fatal("con dueños no debía devolverse pending_ref")
	}
}

func TestResolveOwnersPatEncIADescifraALaPatenteCanonicaAtadaAlAlertID(t *testing.T) {
	iaPublic, iaPrivate := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	// La patente llega con minúsculas y guion: IA debe recibir la forma canónica.
	decryptor := &fakePlateDecryptor{plate: []byte("ab-123 cd")}
	service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	got, err := openWithInfo(iaPrivate, res.PatEncIA, []byte(alertID.String()))
	if err != nil {
		t.Fatalf("pat_enc_ia debía descifrarse con la clave de IA y el alert_id: %v", err)
	}
	if string(got) != "AB123CD" {
		t.Fatalf("pat_enc_ia descifró a %q, esperaba la patente canónica %q", got, "AB123CD")
	}
	if _, err := openWithInfo(iaPrivate, res.PatEncIA, []byte(uuid.NewString())); err == nil {
		t.Fatal("pat_enc_ia no debía descifrarse con el info de otra alerta")
	}
	if _, err := openWithInfo(iaPrivate, res.PatEncIA, nil); err == nil {
		t.Fatal("pat_enc_ia no debía descifrarse con info vacío")
	}
}

// Wiring del caso de uso: cifra con la clave del Encryptor inyectado (la de IA) y no
// con ninguna otra, por ejemplo la de Identity.
func TestResolveOwnersCifraConLaClaveDeIAYNoConLaDeIdentity(t *testing.T) {
	iaPublic, iaPrivate := newKeyPair(t)
	_, identityPrivate := newKeyPair(t)
	alertID := uuid.New()
	info := []byte(alertID.String())
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if _, err := openWithInfo(iaPrivate, res.PatEncIA, info); err != nil {
		t.Fatalf("debía descifrarse con la clave de IA: %v", err)
	}
	if _, err := openWithInfo(identityPrivate, res.PatEncIA, info); err == nil {
		t.Fatal("no debía descifrarse con la clave de Identity")
	}
}

func TestResolveOwnersCifraConInfoIgualAlAlertID(t *testing.T) {
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	if _, err := service.ResolveOwners(context.Background(), "criptograma", alertID, ""); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if string(encryptor.infoRecibido) != alertID.String() {
		t.Fatalf("info esperado %q, obtuvo %q", alertID.String(), encryptor.infoRecibido)
	}
}

func TestResolveOwnersCadaLlamadaProduceUnCriptogramaNuevo(t *testing.T) {
	iaPublic, _ := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	encryptor := newRealEncryptor(t, iaPublic)

	a, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL).
		ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	b, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL).
		ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if a.PatEncIA == b.PatEncIA {
		t.Fatal("pat_enc_ia no debía repetirse entre llamadas")
	}
}

func TestResolveOwnersPatenteSinDuenosRetornaArrayVacioPatEncIAYPendingRef(t *testing.T) {
	iaPublic, iaPrivate := newKeyPair(t)
	alertID := uuid.New()
	pendingRef := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{}, pendingRef: &pendingRef}
	decryptor := &fakePlateDecryptor{plate: []byte("ab-123 cd")}
	service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")

	if err != nil {
		t.Fatalf("un array vacío es un resultado válido, no un error: %v", err)
	}
	if len(res.OwnerIDs) != 0 {
		t.Fatalf("esperaba un slice vacío, obtuvo %d elementos", len(res.OwnerIDs))
	}
	if res.PendingRef == nil || *res.PendingRef != pendingRef {
		t.Fatalf("esperaba el pending_ref %v del repositorio, obtuvo %v", pendingRef, res.PendingRef)
	}
	if res.PatEncIA == "" {
		t.Fatal("sin dueños también debía generarse pat_enc_ia")
	}
	got, err := openWithInfo(iaPrivate, res.PatEncIA, []byte(alertID.String()))
	if err != nil {
		t.Fatalf("pat_enc_ia debía descifrarse con la clave de IA y el alert_id: %v", err)
	}
	if string(got) != "AB123CD" {
		t.Fatalf("pat_enc_ia descifró a %q, esperaba la patente canónica %q", got, "AB123CD")
	}
}

func TestResolveOwnersProponeUnPendingRefNuevoYElVencimientoDeLaVentana(t *testing.T) {
	repo := &fakeRepository{}
	service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), 6*time.Hour)
	ahora := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return ahora }

	if _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), ""); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if repo.propuesta.Ref == uuid.Nil {
		t.Fatal("la propuesta debía llevar un pending_ref nuevo")
	}
	if !repo.propuesta.Now.Equal(ahora) {
		t.Fatalf("Now = %v, esperaba %v", repo.propuesta.Now, ahora)
	}
	if want := ahora.Add(6 * time.Hour); !repo.propuesta.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, esperaba now + PENDING_DELIVERY_HOURS = %v", repo.propuesta.ExpiresAt, want)
	}
}

func TestResolveOwnersCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{err: errors.New("criptograma corrupto")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	_, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con un criptograma indesencriptable")
	}
	if encryptor.invocado {
		t.Fatal("no debía invocarse el cifrador con un criptograma indesencriptable")
	}
}

func TestResolveOwnersPatenteInvalidaRetornaErrInvalidPlateSinConsultarRepositorio(t *testing.T) {
	repo := &fakeRepository{}
	plate := []byte("NOESVALIDA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	_, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con una patente de formato inválido")
	}
	if encryptor.invocado {
		t.Fatal("no debía invocarse el cifrador con una patente de formato inválido")
	}
	assertWiped(t, "la patente en claro", plate)
}

func TestResolveOwnersErrorDelRepositorioCortaAntesDeCifrar(t *testing.T) {
	repo := &fakeRepository{ownersErr: errors.New("fallo de base de datos")}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	if err == nil {
		t.Fatal("esperaba propagar el error del repositorio")
	}
	if res.OwnerIDs != nil || res.PatEncIA != "" || res.PendingRef != nil {
		t.Fatal("ante un error no debía devolverse una respuesta parcial")
	}
	if encryptor.invocado {
		t.Fatal("no debía invocarse el cifrador ante un error del repositorio")
	}
}

func TestResolveOwnersErrorDeCifradoEsInternoYSinRespuestaParcial(t *testing.T) {
	for nombre, owners := range map[string][]uuid.UUID{"con dueños": {uuid.New()}, "sin dueños": {}} {
		t.Run(nombre, func(t *testing.T) {
			pendingRef := uuid.New()
			repo := &fakeRepository{owners: owners, pendingRef: &pendingRef}
			decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
			encryptor := &fakePlateEncryptor{err: errors.New("fallo de cifrado")}
			service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

			res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

			if err == nil {
				t.Fatal("esperaba un error ante el fallo de cifrado")
			}
			for _, domainErr := range []error{domain.ErrInvalidPlate, domain.ErrInvalidCryptogram, domain.ErrRepositoryUnavailable} {
				if errors.Is(err, domainErr) {
					t.Fatalf("el fallo de cifrado no debía mapear a un error de dominio (%v): debe ser interno", domainErr)
				}
			}
			if res.OwnerIDs != nil || res.PatEncIA != "" || res.PendingRef != nil {
				t.Fatal("ante un error de cifrado no debía devolverse una respuesta parcial")
			}
		})
	}
}

func TestResolveOwnersWipeaLaPatenteEnClaroYLaCanonicaEnExito(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	if _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), ""); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica", encryptor.plaintext)
}

func TestResolveOwnersWipeaLaPatenteAnteErrorDelRepositorio(t *testing.T) {
	repo := &fakeRepository{ownersErr: errors.New("fallo de base de datos")}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewResolveOwnersService(repo, decryptor, &fakePlateEncryptor{}, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	_, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	assertWiped(t, "la patente en claro", plate)
}

func TestResolveOwnersWipeaLaPatenteAnteErrorDeCifrado(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{err: errors.New("fallo de cifrado")}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	_, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica", encryptor.plaintext)
}

func TestResolveOwnersWipeaLaPatenteSinDuenos(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, &fakePlateEncryptor{}, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	_, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica", encryptor.plaintext)
}
