package vehicle

import (
	"context"
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

type fakeOwnershipVerifier struct {
	match bool
	err   error
}

func (f *fakeOwnershipVerifier) VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error) {
	return f.match, f.err
}

type fakePlateDecryptor struct {
	plate []byte
	err   error
}

func (f *fakePlateDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.plate, f.err
}

type fakePlateCipher struct {
	ciphertext string
	err        error
}

func (f *fakePlateCipher) Encrypt(plaintext []byte) (string, error) {
	return f.ciphertext, f.err
}

func entradaDePrueba(ownerID uuid.UUID) RegisterVehicleInput {
	return RegisterVehicleInput{OwnerID: ownerID, DNIEnc: []byte("criptograma-dni"), PatEnc: "criptograma-patente"}
}

func TestRegisterOwnershipMismatchNoTocaLaBD(t *testing.T) {
	repo := &fakeRepository{}
	service := NewRegisterVehicleService(repo, &fakeOwnershipVerifier{match: false}, &fakePlateDecryptor{}, &fakePlateCipher{}, []byte("secreto"))

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
	verifier := &fakeOwnershipVerifier{err: domain.ErrOwnershipUnavailable}
	service := NewRegisterVehicleService(repo, verifier, &fakePlateDecryptor{}, &fakePlateCipher{}, []byte("secreto"))

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
	verifier := &fakeOwnershipVerifier{match: true}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	cipher := &fakePlateCipher{ciphertext: "cifrado"}
	service := NewRegisterVehicleService(repo, verifier, decryptor, cipher, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrDuplicateVehicle) {
		t.Fatalf("esperaba domain.ErrDuplicateVehicle, obtuvo %v", err)
	}
}

func TestRegisterMismaPatenteDistintoOwnerSePermite(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	cipher := &fakePlateCipher{ciphertext: "cifrado"}
	service := NewRegisterVehicleService(repo, verifier, decryptor, cipher, []byte("secreto"))

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

func TestRegisterPatenteInvalidaRetornaErrInvalidPlate(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	decryptor := &fakePlateDecryptor{plate: []byte("NOESVALIDA")}
	service := NewRegisterVehicleService(repo, verifier, decryptor, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse una patente con formato inválido")
	}
}

func TestRegisterCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	decryptor := &fakePlateDecryptor{err: errors.New("criptograma corrupto")}
	service := NewRegisterVehicleService(repo, verifier, decryptor, &fakePlateCipher{}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse ante un criptograma de patente inválido")
	}
}

func TestRegisterWipeaLaPatenteEnClaroEnExito(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	plate := []byte("AA000AA")
	service := NewRegisterVehicleService(repo, verifier, &fakePlateDecryptor{plate: plate}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	if err := service.Register(context.Background(), entradaDePrueba(uuid.New())); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado tras el alta: %d", i, b)
		}
	}
}

func TestRegisterWipeaLaPatenteEnClaroAnteErrorDePersistencia(t *testing.T) {
	repo := &fakeRepository{createErr: errors.New("fallo de base de datos")}
	verifier := &fakeOwnershipVerifier{match: true}
	plate := []byte("AA000AA")
	service := NewRegisterVehicleService(repo, verifier, &fakePlateDecryptor{plate: plate}, &fakePlateCipher{ciphertext: "cifrado"}, []byte("secreto"))

	_ = service.Register(context.Background(), entradaDePrueba(uuid.New()))

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado ante error de persistencia: %d", i, b)
		}
	}
}

func TestRegisterWipeaLaPatenteEnClaroAnteErrorDeCifrado(t *testing.T) {
	repo := &fakeRepository{}
	verifier := &fakeOwnershipVerifier{match: true}
	plate := []byte("AA000AA")
	service := NewRegisterVehicleService(repo, verifier, &fakePlateDecryptor{plate: plate}, &fakePlateCipher{err: errors.New("fallo de cifrado")}, []byte("secreto"))

	err := service.Register(context.Background(), entradaDePrueba(uuid.New()))
	if err == nil {
		t.Fatal("esperaba error de cifrado")
	}
	if repo.callCount != 0 {
		t.Fatal("no debía persistirse ante un fallo de cifrado")
	}

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado ante error de cifrado: %d", i, b)
		}
	}
}
