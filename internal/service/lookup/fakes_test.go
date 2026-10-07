package lookup

import (
	"context"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// fakeRepository es compartido por las pruebas de ResolveOwnersService.
type fakeRepository struct {
	owners          []uuid.UUID
	ownersErr       error
	patHashRecibido string
}

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	return nil
}

func (f *fakeRepository) FindOwnersByPatHash(ctx context.Context, patHashed string) ([]uuid.UUID, error) {
	f.patHashRecibido = patHashed
	return f.owners, f.ownersErr
}

// fakePlateDecryptor es compartido por las pruebas de ResolveOwnersService.
type fakePlateDecryptor struct {
	plate []byte
	err   error
}

func (f *fakePlateDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.plate, f.err
}
