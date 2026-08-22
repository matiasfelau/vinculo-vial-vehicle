// Package vehicle implementa el caso de uso de vinculación de vehículo:
// verificar propiedad -> desencriptar -> validar -> hashear -> cifrar -> persistir.
package vehicle

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/symmetric"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/secure"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/validation"
)

// Verificación en tiempo de compilación de que las implementaciones concretas de
// producción satisfacen las interfaces requeridas por el caso de uso.
var (
	_ PlateDecryptor = (*hpke.Decryptor)(nil)
	_ PlateCipher    = (*symmetric.Cipher)(nil)
)

// PlateDecryptor desencripta pat_enc con la clave privada HPKE de este servicio.
type PlateDecryptor interface {
	Decrypt(ciphertextBase64 string) ([]byte, error)
}

// PlateCipher cifra la patente en claro para su persistencia at-rest.
type PlateCipher interface {
	Encrypt(plaintext []byte) (string, error)
}

// RegisterVehicleInput agrupa los datos recibidos para vincular un vehículo.
type RegisterVehicleInput struct {
	OwnerID uuid.UUID
	DNIEnc  []byte
	PatEnc  string
}

// RegisterVehicleService orquesta el caso de uso US-04.
type RegisterVehicleService struct {
	repository        domain.VehicleRepository
	ownershipVerifier domain.OwnershipVerifier
	plateDecryptor    PlateDecryptor
	plateCipher       PlateCipher
	hmacSecret        []byte
}

// NewRegisterVehicleService construye el caso de uso con sus dependencias.
func NewRegisterVehicleService(
	repository domain.VehicleRepository,
	ownershipVerifier domain.OwnershipVerifier,
	plateDecryptor PlateDecryptor,
	plateCipher PlateCipher,
	hmacSecret []byte,
) *RegisterVehicleService {
	return &RegisterVehicleService{
		repository:        repository,
		ownershipVerifier: ownershipVerifier,
		plateDecryptor:    plateDecryptor,
		plateCipher:       plateCipher,
		hmacSecret:        hmacSecret,
	}
}

// Register ejecuta el flujo completo de vinculación. dniEnc se reenvía a Identity
// sin abrir; solo si Identity confirma la propiedad se desencripta la patente.
func (s *RegisterVehicleService) Register(ctx context.Context, input RegisterVehicleInput) error {
	match, err := s.ownershipVerifier.VerifyOwnership(ctx, input.OwnerID, input.DNIEnc)
	if err != nil {
		return err
	}
	if !match {
		return domain.ErrOwnershipMismatch
	}

	plate, err := s.plateDecryptor.Decrypt(input.PatEnc)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(plate)

	normalized := validation.NormalizePlate(plate)
	defer secure.Wipe(normalized)

	if err := validation.ValidatePlate(normalized); err != nil {
		return err
	}

	patHashed := blindindex.Compute(s.hmacSecret, normalized)

	patAES, err := s.plateCipher.Encrypt(normalized)
	if err != nil {
		return fmt.Errorf("no se pudo cifrar la patente: %w", err)
	}

	vehicleToCreate := &domain.Vehicle{
		ID:        uuid.New(),
		OwnerID:   input.OwnerID,
		PatHashed: patHashed,
		PatAES:    patAES,
		CreatedAt: time.Now().UTC(),
	}

	return s.repository.Create(ctx, vehicleToCreate)
}
