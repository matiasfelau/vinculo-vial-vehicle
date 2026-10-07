package vehicle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

type fakeRepository struct {
	createErr error
	created   *domain.Vehicle
	callCount int
}

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	f.callCount++
	f.created = vehicle
	return f.createErr
}

func (f *fakeRepository) FindOwnersByPatHash(ctx context.Context, patHashed string) ([]uuid.UUID, error) {
	return nil, nil
}

type fakeOwnershipVerifier struct {
	match       bool
	err         error
	fueInvocado bool
}

func (f *fakeOwnershipVerifier) VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error) {
	f.fueInvocado = true
	return f.match, f.err
}

type fakeDNRPAClient struct {
	dni         []byte
	err         error
	fueInvocado bool
}

func (f *fakeDNRPAClient) GetOwnerDNI(ctx context.Context, plate []byte) ([]byte, error) {
	f.fueInvocado = true
	return f.dni, f.err
}

type fakePayloadDecryptor struct {
	payload []byte
	err     error
}

func (f *fakePayloadDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.payload, f.err
}

type fakeDNIEncryptor struct {
	ciphertext []byte
	err        error
}

func (f *fakeDNIEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	return f.ciphertext, f.err
}

type fakePlateCipher struct {
	ciphertext string
	err        error
}

func (f *fakePlateCipher) Encrypt(plaintext []byte) (string, error) {
	return f.ciphertext, f.err
}

func payloadValido(t *testing.T, patente, chasis string) []byte {
	t.Helper()

	raw, err := json.Marshal(vehiclePayload{Patente: []byte(patente), Chasis: []byte(chasis)})
	if err != nil {
		t.Fatalf("no se pudo serializar el payload de prueba: %v", err)
	}
	return raw
}

func entradaDePrueba(ownerID uuid.UUID) RegisterVehicleInput {
	return RegisterVehicleInput{OwnerID: ownerID, PayloadEnc: "criptograma-payload"}
}

func TestRegisterPayloadCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	dnrpa := &fakeDNRPAClient{}
	decryptor := &fakePayloadDecryptor{err: errors.New("criptograma corrupto")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{}, dnrpa, decryptor, &fakeDNIEncryptor{}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if dnrpa.fueInvocado {
		t.Fatal("no debía consultarse el DNRPA con un payload indesencriptable")
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterPayloadConJSONMalformadoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: []byte("no es json")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{}, &fakeDNRPAClient{}, decryptor, &fakeDNIEncryptor{}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterPatenteInvalidaRetornaErrInvalidPlateSinConsultarDNRPA(t *testing.T) {
	repo := &fakeRepository{}
	dnrpa := &fakeDNRPAClient{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "NOESVALIDA", "1HGCM82633A004352")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{}, dnrpa, decryptor, &fakeDNIEncryptor{}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
	if dnrpa.fueInvocado {
		t.Fatal("no debía consultarse el DNRPA con una patente de formato inválido")
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterDNRPAInalcanzableRetornaErrOwnerLookupUnavailable(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{err: errors.New("dnrpa caído")}
	service := NewRegisterVehicleService(repo, verifier, dnrpa, decryptor, &fakeDNIEncryptor{}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrOwnerLookupUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnerLookupUnavailable, obtuvo %v", err)
	}
	if verifier.fueInvocado {
		t.Fatal("no debía verificarse propiedad sin poder consultar al DNRPA")
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterOwnershipMismatchNoTocaLaBD(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: false}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrOwnershipMismatch) {
		t.Fatalf("esperaba domain.ErrOwnershipMismatch, obtuvo %v", err)
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada cuando Identity reporta match: false")
	}
}

func TestRegisterIdentityInalcanzableNoPersiste(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	verifier := &fakeOwnershipVerifier{err: domain.ErrOwnershipUnavailable}
	service := NewRegisterVehicleService(repo, verifier, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnershipUnavailable, obtuvo %v", err)
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada cuando Identity no responde")
	}
}

func TestRegisterAltaDuplicadaDelMismoOwner(t *testing.T) {
	repo := &fakeRepository{createErr: domain.ErrDuplicateVehicle}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: true}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrDuplicateVehicle) {
		t.Fatalf("esperaba domain.ErrDuplicateVehicle, obtuvo %v", err)
	}
}

func TestRegisterMismaPatenteDistintoOwnerSePermite(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: true}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	ownerID := uuid.New()
	if err := service.Register(context.Background(), entradaDePrueba(ownerID)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if repo.callCount != 1 {
		t.Fatalf("esperaba exactamente una persistencia, hubo %d", repo.callCount)
	}
	if repo.created.OwnerID != ownerID {
		t.Fatal("el owner_id persistido no coincide con el de la solicitud")
	}
	if repo.created.PatHashed == "" || repo.created.PatAES == "" {
		t.Fatal("el vehículo persistido debe incluir pat_hashed y pat_aes")
	}
}

func TestRegisterCifradoDeDNIInvalidoNoPersiste(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, verifier, dnrpa, decryptor, &fakeDNIEncryptor{err: errors.New("clave pública inválida")}, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if err == nil {
		t.Fatal("esperaba error al no poder cifrar el dni")
	}
	if verifier.fueInvocado {
		t.Fatal("no debía verificarse propiedad sin poder cifrar el dni")
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterWipeaElPayloadEnClaroEnExito(t *testing.T) {
	repo := &fakeRepository{}
	payload := payloadValido(t, "AA000AA", "1HGCM82633A004352")
	decryptor := &fakePayloadDecryptor{payload: payload}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: true}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	if err := service.Register(context.Background(), entradaDePrueba(uuid.New())); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i, b := range payload {
		if b != 0 {
			t.Fatalf("byte %d del payload en claro no fue purgado tras el alta: %d", i, b)
		}
	}
}

func TestRegisterWipeaElDNIEnClaroEnExito(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dni := []byte("12345678")
	dnrpa := &fakeDNRPAClient{dni: dni}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: true}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	if err := service.Register(context.Background(), entradaDePrueba(uuid.New())); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i, b := range dni {
		if b != 0 {
			t.Fatalf("byte %d del dni en claro no fue purgado tras el alta: %d", i, b)
		}
	}
}

func TestRegisterWipeaElDNIEnClaroAnteFalloDeCifrado(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePayloadDecryptor{payload: payloadValido(t, "AA000AA", "1HGCM82633A004352")}
	dni := []byte("12345678")
	dnrpa := &fakeDNRPAClient{dni: dni}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{}, dnrpa, decryptor, &fakeDNIEncryptor{err: errors.New("fallo de cifrado")}, &fakePlateCipher{}, []byte("secreto"))

	_ = service.Register(context.Background(), entradaDePrueba(uuid.New()))

	for i, b := range dni {
		if b != 0 {
			t.Fatalf("byte %d del dni en claro no fue purgado ante error de cifrado: %d", i, b)
		}
	}
}

func TestRegisterWipeaElPayloadEnClaroAnteErrorDePersistencia(t *testing.T) {
	repo := &fakeRepository{createErr: errors.New("fallo de base de datos")}
	payload := payloadValido(t, "AA000AA", "1HGCM82633A004352")
	decryptor := &fakePayloadDecryptor{payload: payload}
	dnrpa := &fakeDNRPAClient{dni: []byte("12345678")}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: true}, dnrpa, decryptor, &fakeDNIEncryptor{ciphertext: []byte("dni-cifrado")}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	_ = service.Register(context.Background(), entradaDePrueba(uuid.New()))

	for i, b := range payload {
		if b != 0 {
			t.Fatalf("byte %d del payload en claro no fue purgado ante error de persistencia: %d", i, b)
		}
	}
}
