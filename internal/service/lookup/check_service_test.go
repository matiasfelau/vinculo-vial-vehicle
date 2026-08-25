package lookup

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// fakeRepository es compartido por las pruebas de CheckVehicleService y
// ResolveOwnersService en este mismo paquete.
type fakeRepository struct {
	exists          bool
	existsErr       error
	owners          []uuid.UUID
	ownersErr       error
	patHashRecibido string
}

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	return nil
}

func (f *fakeRepository) ExistsByPatHash(ctx context.Context, patHashed string) (bool, error) {
	f.patHashRecibido = patHashed
	return f.exists, f.existsErr
}

func (f *fakeRepository) FindOwnersByPatHash(ctx context.Context, patHashed string) ([]uuid.UUID, error) {
	f.patHashRecibido = patHashed
	return f.owners, f.ownersErr
}

// fakePlateDecryptor es compartido por las pruebas de CheckVehicleService y
// ResolveOwnersService en este mismo paquete.
type fakePlateDecryptor struct {
	plate []byte
	err   error
}

func (f *fakePlateDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.plate, f.err
}

func TestCheckPatenteExistenteRetornaTrue(t *testing.T) {
	repo := &fakeRepository{exists: true}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	exists, err := service.Check(context.Background(), "criptograma")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !exists {
		t.Fatal("esperaba true para una patente vinculada")
	}
}

func TestCheckPatenteInexistenteRetornaFalse(t *testing.T) {
	repo := &fakeRepository{exists: false}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	exists, err := service.Check(context.Background(), "criptograma")

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if exists {
		t.Fatal("esperaba false para una patente no vinculada")
	}
}

func TestCheckCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{err: errors.New("criptograma corrupto")}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	_, err := service.Check(context.Background(), "criptograma")

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con un criptograma indesencriptable")
	}
}

func TestCheckPatenteInvalidaRetornaErrInvalidPlateSinConsultarRepositorio(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{plate: []byte("NOESVALIDA")}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	_, err := service.Check(context.Background(), "criptograma")

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con una patente de formato inválido")
	}
}

func TestCheckErrorDelRepositorioSePropaga(t *testing.T) {
	repo := &fakeRepository{existsErr: errors.New("fallo de base de datos")}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	_, err := service.Check(context.Background(), "criptograma")

	if err == nil {
		t.Fatal("esperaba propagar el error del repositorio")
	}
}

func TestCheckWipeaLaPatenteEnClaroEnExito(t *testing.T) {
	repo := &fakeRepository{exists: true}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	if _, err := service.Check(context.Background(), "criptograma"); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado: %d", i, b)
		}
	}
}

func TestCheckWipeaLaPatenteEnClaroAnteErrorDelRepositorio(t *testing.T) {
	repo := &fakeRepository{existsErr: errors.New("fallo de base de datos")}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewCheckVehicleService(repo, decryptor, []byte("secreto"))

	_, _ = service.Check(context.Background(), "criptograma")

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado ante error: %d", i, b)
		}
	}
}
