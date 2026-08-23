package domain

import (
	"context"

	"github.com/google/uuid"
)

// OwnershipVerifier define la verificación de propiedad contra Identity.
// dniEnc es el DNI obtenido del DNRPA, cifrado por este servicio con la clave
// pública HPKE de Identity antes de esta llamada.
type OwnershipVerifier interface {
	VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error)
}
