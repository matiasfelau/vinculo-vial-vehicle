package lookup

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

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

func TestResolveOwnersPatenteConTresDuenosRetornaLosTresUUIDs(t *testing.T) {
	owners := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	repo := &fakeRepository{owners: owners}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, &fakePlateEncryptor{}, []byte("secreto"))

	result, _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("esperaba 3 dueños, obtuvo %d", len(result))
	}
	for i, ownerID := range owners {
		if result[i] != ownerID {
			t.Fatalf("owner_id en posición %d = %v, esperaba %v", i, result[i], ownerID)
		}
	}
}

func TestResolveOwnersPatEncIADescifraALaPatenteCanonicaAtadaAlAlertID(t *testing.T) {
	iaPublic, iaPrivate := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	// La patente llega con minúsculas y guion: IA debe recibir la forma canónica.
	decryptor := &fakePlateDecryptor{plate: []byte("ab-123 cd")}
	service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), []byte("secreto"))

	_, patEncIA, err := service.ResolveOwners(context.Background(), "criptograma", alertID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	got, err := openWithInfo(iaPrivate, patEncIA, []byte(alertID.String()))
	if err != nil {
		t.Fatalf("pat_enc_ia debía descifrarse con la clave de IA y el alert_id: %v", err)
	}
	if string(got) != "AB123CD" {
		t.Fatalf("pat_enc_ia descifró a %q, esperaba la patente canónica %q", got, "AB123CD")
	}
	if _, err := openWithInfo(iaPrivate, patEncIA, []byte(uuid.NewString())); err == nil {
		t.Fatal("pat_enc_ia no debía descifrarse con el info de otra alerta")
	}
	if _, err := openWithInfo(iaPrivate, patEncIA, nil); err == nil {
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
	service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), []byte("secreto"))

	_, patEncIA, err := service.ResolveOwners(context.Background(), "criptograma", alertID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if _, err := openWithInfo(iaPrivate, patEncIA, info); err != nil {
		t.Fatalf("debía descifrarse con la clave de IA: %v", err)
	}
	if _, err := openWithInfo(identityPrivate, patEncIA, info); err == nil {
		t.Fatal("no debía descifrarse con la clave de Identity")
	}
}

func TestResolveOwnersCifraConInfoIgualAlAlertID(t *testing.T) {
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	if _, _, err := service.ResolveOwners(context.Background(), "criptograma", alertID); err != nil {
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

	_, a, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, encryptor, []byte("secreto")).
		ResolveOwners(context.Background(), "criptograma", alertID)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	_, b, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, encryptor, []byte("secreto")).
		ResolveOwners(context.Background(), "criptograma", alertID)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if a == b {
		t.Fatal("pat_enc_ia no debía repetirse entre llamadas")
	}
}

func TestResolveOwnersPatenteInexistenteRetornaArrayVacioSinPatEncIA(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	result, patEncIA, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	if err != nil {
		t.Fatalf("un array vacío es un resultado válido, no un error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("esperaba un slice vacío, obtuvo %d elementos", len(result))
	}
	if patEncIA != "" {
		t.Fatal("sin dueños no debía generarse pat_enc_ia")
	}
	if encryptor.invocado {
		t.Fatal("sin dueños no debía invocarse el cifrador")
	}
}

func TestResolveOwnersCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{err: errors.New("criptograma corrupto")}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	_, _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

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
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	_, _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

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
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	result, patEncIA, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	if err == nil {
		t.Fatal("esperaba propagar el error del repositorio")
	}
	if result != nil || patEncIA != "" {
		t.Fatal("ante un error no debía devolverse una respuesta parcial")
	}
	if encryptor.invocado {
		t.Fatal("no debía invocarse el cifrador ante un error del repositorio")
	}
}

func TestResolveOwnersErrorDeCifradoEsInternoYSinRespuestaParcial(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	encryptor := &fakePlateEncryptor{err: errors.New("fallo de cifrado")}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	result, patEncIA, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	if err == nil {
		t.Fatal("esperaba un error ante el fallo de cifrado")
	}
	for _, domainErr := range []error{domain.ErrInvalidPlate, domain.ErrInvalidCryptogram, domain.ErrRepositoryUnavailable} {
		if errors.Is(err, domainErr) {
			t.Fatalf("el fallo de cifrado no debía mapear a un error de dominio (%v): debe ser interno", domainErr)
		}
	}
	if result != nil || patEncIA != "" {
		t.Fatal("ante un error de cifrado no debía devolverse una respuesta parcial")
	}
}

func TestResolveOwnersWipeaLaPatenteEnClaroYLaCanonicaEnExito(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	if _, _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New()); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica", encryptor.plaintext)
}

func TestResolveOwnersWipeaLaPatenteAnteErrorDelRepositorio(t *testing.T) {
	repo := &fakeRepository{ownersErr: errors.New("fallo de base de datos")}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewResolveOwnersService(repo, decryptor, &fakePlateEncryptor{}, []byte("secreto"))

	_, _, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	assertWiped(t, "la patente en claro", plate)
}

func TestResolveOwnersWipeaLaPatenteAnteErrorDeCifrado(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	encryptor := &fakePlateEncryptor{err: errors.New("fallo de cifrado")}
	service := NewResolveOwnersService(repo, decryptor, encryptor, []byte("secreto"))

	_, _, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica", encryptor.plaintext)
}

func TestResolveOwnersWipeaLaPatenteSinDuenos(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewResolveOwnersService(repo, decryptor, &fakePlateEncryptor{}, []byte("secreto"))

	_, _, _ = service.ResolveOwners(context.Background(), "criptograma", uuid.New())

	assertWiped(t, "la patente en claro", plate)
}
