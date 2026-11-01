package lookup

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// fakeRepository es compartido por las pruebas de ResolveOwnersService.
type fakeRepository struct {
	owners          []uuid.UUID
	pendingRef      *uuid.UUID
	ownersErr       error
	patHashRecibido string
	propuesta       domain.PendingProposal
}

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	return nil
}

func (f *fakeRepository) FindOwnersOrReservePending(ctx context.Context, patHashed string, proposal domain.PendingProposal) ([]uuid.UUID, *uuid.UUID, error) {
	f.patHashRecibido = patHashed
	f.propuesta = proposal
	return f.owners, f.pendingRef, f.ownersErr
}

func (f *fakeRepository) DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}

// fakePlateDecryptor es compartido por las pruebas de ResolveOwnersService.
type fakePlateDecryptor struct {
	plate []byte
	err   error
}

func (f *fakePlateDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.plate, f.err
}
