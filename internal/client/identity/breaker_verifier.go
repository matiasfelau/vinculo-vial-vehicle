package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/breaker"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// Verificación en tiempo de compilación de que BreakerVerifier satisface la
// misma interfaz que decora.
var _ domain.OwnershipVerifier = (*BreakerVerifier)(nil)

// BreakerVerifier decora un domain.OwnershipVerifier con un circuit breaker de
// fail-fast: con el circuito abierto, la llamada falla de inmediato sin
// alcanzar a identity. Solo cuentan como falla contabilizable las categorías
// de indisponibilidad real que produce Client.VerifyOwnership
// (ErrOwnershipUnavailable y ErrOwnershipUpstreamFailure); un rechazo por 4xx
// se trata como éxito de disponibilidad, ya que identity sí respondió.
type BreakerVerifier struct {
	verifier domain.OwnershipVerifier
	cb       *breaker.Breaker
}

// NewBreakerVerifier construye el decorador sobre el verificador y el breaker dados.
func NewBreakerVerifier(verifier domain.OwnershipVerifier, cb *breaker.Breaker) *BreakerVerifier {
	return &BreakerVerifier{verifier: verifier, cb: cb}
}

// VerifyOwnership aplica el breaker antes de delegar en el verificador decorado.
func (b *BreakerVerifier) VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error) {
	if !b.cb.Allow() {
		return false, domain.ErrOwnershipCircuitOpen
	}

	match, err := b.verifier.VerifyOwnership(ctx, ownerID, dniEnc)
	if esFallaContabilizable(err) {
		b.cb.RecordFailure()
	} else {
		b.cb.RecordSuccess()
	}

	return match, err
}

// esFallaContabilizable determina si err representa una indisponibilidad real
// de identity (nunca respondió, o respondió con 5xx/cuerpo inválido). Un 4xx
// (rechazo por contenido de la solicitud) o un éxito no cuentan: el destino
// demostró estar disponible al responder.
func esFallaContabilizable(err error) bool {
	return errors.Is(err, domain.ErrOwnershipUnavailable) || errors.Is(err, domain.ErrOwnershipUpstreamFailure)
}
