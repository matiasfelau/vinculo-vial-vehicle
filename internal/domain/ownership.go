package domain

import (
	"context"

	"github.com/google/uuid"
)

// OwnershipVerifier define la verificación de propiedad contra Identity.
// El criptograma dniEnc se reenvía sin abrir: este servicio no posee la clave para desencriptarlo.
type OwnershipVerifier interface {
	VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error)
}
