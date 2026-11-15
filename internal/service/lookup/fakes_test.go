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

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle, maxPerOwner int) (int, error) {
	return 0, nil
}

func (f *fakeRepository) CountByOwner(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return 0, nil
}

func (f *fakeRepository) DeleteByOwnerAndPlate(ctx context.Context, ownerID uuid.UUID, patHashed string) (int, error) {
	return 0, nil
}

func (f *fakeRepository) DeleteByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
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
