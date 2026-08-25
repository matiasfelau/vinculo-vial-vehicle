package lookup

import (
	"context"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// ResolveOwnersService orquesta US-05: POST /internal/resolve-owner.
// Solo debe alcanzarlo vinculo-vial-alert, a través del router interno
// aislado (INTERNAL_PORT + X-Internal-Service-Token).
type ResolveOwnersService struct {
	repository domain.VehicleRepository
	decryptor  PlateDecryptor
	hmacSecret []byte
}

// NewResolveOwnersService construye el caso de uso con sus dependencias.
func NewResolveOwnersService(repository domain.VehicleRepository, decryptor PlateDecryptor, hmacSecret []byte) *ResolveOwnersService {
	return &ResolveOwnersService{repository: repository, decryptor: decryptor, hmacSecret: hmacSecret}
}

// ResolveOwners desencripta patEnc y retorna todos los owner_id vinculados a
// esa patente (relación 1 patente -> N dueños). Un slice vacío es un
// resultado válido, no un error: significa que la patente no está en la red,
// y así debe tratarlo el llamador.
func (s *ResolveOwnersService) ResolveOwners(ctx context.Context, patEnc string) ([]uuid.UUID, error) {
	patHashed, err := resolvePatHash(s.decryptor, s.hmacSecret, patEnc)
	if err != nil {
		return nil, err
	}

	return s.repository.FindOwnersByPatHash(ctx, patHashed)
}
