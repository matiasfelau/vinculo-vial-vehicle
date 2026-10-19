package lookup

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/secure"
)

// PlateEncryptor cifra la patente canónica con la clave pública HPKE del servicio
// destinatario (IA), atando el criptograma al info recibido. Es una interfaz
// separada de la que cifra el DNI hacia Identity: cada una se instancia con la
// clave de su destinatario.
type PlateEncryptor interface {
	EncryptWithInfo(plaintext, info []byte) ([]byte, error)
}

// Verificación en tiempo de compilación de que la implementación concreta de
// producción satisface PlateEncryptor.
var _ PlateEncryptor = (*hpke.Encryptor)(nil)

// ResolveOwnersService orquesta US-05, US-08 y US-10: POST /internal/resolve-owner.
// Solo debe alcanzarlo vinculo-vial-alert, a través del router interno
// aislado (INTERNAL_PORT + X-Internal-Service-Token).
type ResolveOwnersService struct {
	repository      domain.VehicleRepository
	decryptor       PlateDecryptor
	encryptor       PlateEncryptor
	hmacSecret      []byte
	pendingDelivery time.Duration
	now             func() time.Time
}

// NewResolveOwnersService construye el caso de uso con sus dependencias. El
// encryptor debe estar construido con la clave pública de IA. pendingDelivery
// es PENDING_DELIVERY_HOURS: cuánto tiempo conserva Vehicle la reserva de una
// patente sin dueños, contado desde la última alerta.
func NewResolveOwnersService(repository domain.VehicleRepository, decryptor PlateDecryptor, encryptor PlateEncryptor, hmacSecret []byte, pendingDelivery time.Duration) *ResolveOwnersService {
	return &ResolveOwnersService{
		repository:      repository,
		decryptor:       decryptor,
		encryptor:       encryptor,
		hmacSecret:      hmacSecret,
		pendingDelivery: pendingDelivery,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

// ResolveOwners desencripta patEnc y retorna todos los owner_id vinculados a
// esa patente (relación 1 patente -> N dueños), junto con pat_enc_ia: la patente
// canónica cifrada con HPKE hacia IA, con info = alertID, en base64 de enc‖ct.
//
// pat_enc_ia se genera siempre, también sin dueños (US-10): el emisor nunca se
// entera de si el vehículo está en la red, así que IA valida la patente de la
// misma forma en ambos casos. Sin dueños, además, la patente queda reservada
// como pendiente de entrega por PENDING_DELIVERY_HOURS y se retorna su
// pending_ref; si la patente ya tenía una reserva vigente se reutiliza y se
// extiende. Si tiene al menos un dueño (incluido el propio emisor, que Vehicle
// no conoce) no hay reserva: la patente tiene dueño.
//
// Un slice vacío es un resultado válido, no un error. Un error de cifrado es
// un error interno y nunca devuelve una respuesta parcial. La patente se purga
// de memoria en todos los caminos.
func (s *ResolveOwnersService) ResolveOwners(ctx context.Context, patEnc string, alertID uuid.UUID) (domain.OwnerResolution, error) {
	plate, err := canonicalPlate(s.decryptor, patEnc)
	if err != nil {
		return domain.OwnerResolution{}, err
	}
	defer secure.Wipe(plate)

	now := s.now()
	ownerIDs, pendingRef, err := s.repository.FindOwnersOrReservePending(ctx, blindindex.Compute(s.hmacSecret, plate), domain.PendingProposal{
		Ref:       uuid.New(),
		Now:       now,
		ExpiresAt: now.Add(s.pendingDelivery),
	})
	if err != nil {
		return domain.OwnerResolution{}, err
	}

	sealed, err := s.encryptor.EncryptWithInfo(plate, []byte(alertID.String()))
	if err != nil {
		return domain.OwnerResolution{}, fmt.Errorf("lookup: no se pudo cifrar la patente hacia IA: %w", err)
	}

	return domain.OwnerResolution{
		OwnerIDs:   ownerIDs,
		PatEncIA:   base64.StdEncoding.EncodeToString(sealed),
		PendingRef: pendingRef,
	}, nil
}
