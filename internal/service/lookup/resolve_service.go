package lookup

import (
	"context"
	"encoding/base64"
	"fmt"

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

// ResolveOwnersService orquesta US-05 y US-08: POST /internal/resolve-owner.
// Solo debe alcanzarlo vinculo-vial-alert, a través del router interno
// aislado (INTERNAL_PORT + X-Internal-Service-Token).
type ResolveOwnersService struct {
	repository domain.VehicleRepository
	decryptor  PlateDecryptor
	encryptor  PlateEncryptor
	hmacSecret []byte
}

// NewResolveOwnersService construye el caso de uso con sus dependencias. El
// encryptor debe estar construido con la clave pública de IA.
func NewResolveOwnersService(repository domain.VehicleRepository, decryptor PlateDecryptor, encryptor PlateEncryptor, hmacSecret []byte) *ResolveOwnersService {
	return &ResolveOwnersService{repository: repository, decryptor: decryptor, encryptor: encryptor, hmacSecret: hmacSecret}
}

// ResolveOwners desencripta patEnc y retorna todos los owner_id vinculados a
// esa patente (relación 1 patente -> N dueños), junto con pat_enc_ia: la patente
// canónica cifrada con HPKE hacia IA, con info = alertID, en base64 de enc‖ct.
//
// Un slice vacío es un resultado válido, no un error: significa que la patente
// no está en la red, y así debe tratarlo el llamador; en ese caso no se cifra y
// patEncIA es "". Un error de cifrado es un error interno y nunca devuelve una
// respuesta parcial. La patente se purga de memoria en todos los caminos.
func (s *ResolveOwnersService) ResolveOwners(ctx context.Context, patEnc string, alertID uuid.UUID) ([]uuid.UUID, string, error) {
	plate, err := canonicalPlate(s.decryptor, patEnc)
	if err != nil {
		return nil, "", err
	}
	defer secure.Wipe(plate)

	ownerIDs, err := s.repository.FindOwnersByPatHash(ctx, blindindex.Compute(s.hmacSecret, plate))
	if err != nil {
		return nil, "", err
	}
	if len(ownerIDs) == 0 {
		return ownerIDs, "", nil
	}

	sealed, err := s.encryptor.EncryptWithInfo(plate, []byte(alertID.String()))
	if err != nil {
		return nil, "", fmt.Errorf("lookup: no se pudo cifrar la patente hacia IA: %w", err)
	}

	return ownerIDs, base64.StdEncoding.EncodeToString(sealed), nil
}
