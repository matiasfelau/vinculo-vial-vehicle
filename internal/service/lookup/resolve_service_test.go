package lookup

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func TestResolveOwnersPatenteConTresDuenosRetornaLosTresUUIDs(t *testing.T) {
	owners := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	repo := &fakeRepository{owners: owners}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	result, err := service.ResolveOwners(context.Background(), "criptograma")

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

func TestResolveOwnersPatenteInexistenteRetornaArrayVacioSinError(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{}}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	result, err := service.ResolveOwners(context.Background(), "criptograma")

	if err != nil {
		t.Fatalf("un array vacío es un resultado válido, no un error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("esperaba un slice vacío, obtuvo %d elementos", len(result))
	}
}

func TestResolveOwnersCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{err: errors.New("criptograma corrupto")}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	_, err := service.ResolveOwners(context.Background(), "criptograma")

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con un criptograma indesencriptable")
	}
}

func TestResolveOwnersPatenteInvalidaRetornaErrInvalidPlateSinConsultarRepositorio(t *testing.T) {
	repo := &fakeRepository{}
	decryptor := &fakePlateDecryptor{plate: []byte("NOESVALIDA")}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	_, err := service.ResolveOwners(context.Background(), "criptograma")

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
	if repo.patHashRecibido != "" {
		t.Fatal("no debía consultarse el repositorio con una patente de formato inválido")
	}
}

func TestResolveOwnersErrorDelRepositorioSePropaga(t *testing.T) {
	repo := &fakeRepository{ownersErr: errors.New("fallo de base de datos")}
	decryptor := &fakePlateDecryptor{plate: []byte("AA000AA")}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	_, err := service.ResolveOwners(context.Background(), "criptograma")

	if err == nil {
		t.Fatal("esperaba propagar el error del repositorio")
	}
}

func TestResolveOwnersWipeaLaPatenteEnClaroEnExito(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	if _, err := service.ResolveOwners(context.Background(), "criptograma"); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado: %d", i, b)
		}
	}
}

func TestResolveOwnersWipeaLaPatenteEnClaroAnteErrorDelRepositorio(t *testing.T) {
	repo := &fakeRepository{ownersErr: errors.New("fallo de base de datos")}
	plate := []byte("AA000AA")
	decryptor := &fakePlateDecryptor{plate: plate}
	service := NewResolveOwnersService(repo, decryptor, []byte("secreto"))

	_, _ = service.ResolveOwners(context.Background(), "criptograma")

	for i, b := range plate {
		if b != 0 {
			t.Fatalf("byte %d de la patente en claro no fue purgado ante error: %d", i, b)
		}
	}
}
