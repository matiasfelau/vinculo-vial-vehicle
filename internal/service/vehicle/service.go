// Package vehicle implementa el caso de uso de vinculación de vehículo:
// descifrar payload -> consultar titularidad (DNRPA) -> cifrar DNI -> verificar
// propiedad (Identity) -> validar patente -> hashear -> cifrar -> persistir.
package vehicle

import (
	"context"
	"encoding/json"
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
	_ PayloadDecryptor = (*hpke.Decryptor)(nil)
	_ DNIEncryptor     = (*hpke.Encryptor)(nil)
	_ PlateCipher      = (*symmetric.Cipher)(nil)
)

// PayloadDecryptor desencripta payload_enc con la clave privada HPKE de este servicio.
type PayloadDecryptor interface {
	Decrypt(ciphertextBase64 string) ([]byte, error)
}

// DNIEncryptor cifra el DNI obtenido del DNRPA con la clave pública HPKE de Identity.
type DNIEncryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
}

// PlateCipher cifra la patente en claro para su persistencia at-rest.
type PlateCipher interface {
	Encrypt(plaintext []byte) (string, error)
}

// RegisterVehicleInput agrupa los datos recibidos para vincular un vehículo.
type RegisterVehicleInput struct {
	OwnerID    uuid.UUID
	PayloadEnc string
}

// vehiclePayload es el contenido de payload_enc una vez desencriptado. Los campos
// son []byte (no string) para preservar Zero-Retention: encoding/json decodifica
// el base64 de cada campo directamente a bytes, sin materializar el valor sensible
// como string de Go en ningún punto.
type vehiclePayload struct {
	Patente []byte `json:"patente"`
	Chasis  []byte `json:"chasis"`
}

// RegisterVehicleService orquesta el caso de uso US-04.
type RegisterVehicleService struct {
	repository        domain.VehicleRepository
	ownershipVerifier domain.OwnershipVerifier
	dnrpaClient       domain.DNRPAClient
	payloadDecryptor  PayloadDecryptor
	dniEncryptor      DNIEncryptor
	plateCipher       PlateCipher
	hmacSecret        []byte
}

// NewRegisterVehicleService construye el caso de uso con sus dependencias.
func NewRegisterVehicleService(
	repository domain.VehicleRepository,
	ownershipVerifier domain.OwnershipVerifier,
	dnrpaClient domain.DNRPAClient,
	payloadDecryptor PayloadDecryptor,
	dniEncryptor DNIEncryptor,
	plateCipher PlateCipher,
	hmacSecret []byte,
) *RegisterVehicleService {
	return &RegisterVehicleService{
		repository:        repository,
		ownershipVerifier: ownershipVerifier,
		dnrpaClient:       dnrpaClient,
		payloadDecryptor:  payloadDecryptor,
		dniEncryptor:      dniEncryptor,
		plateCipher:       plateCipher,
		hmacSecret:        hmacSecret,
	}
}

// Register ejecuta el flujo completo de vinculación.
func (s *RegisterVehicleService) Register(ctx context.Context, input RegisterVehicleInput) error {
	payload, err := s.payloadDecryptor.Decrypt(input.PayloadEnc)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(payload)

	var data vehiclePayload
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(data.Patente)
	defer secure.Wipe(data.Chasis)

	normalized := validation.NormalizePlate(data.Patente)
	defer secure.Wipe(normalized)

	if err := validation.ValidatePlate(normalized); err != nil {
		return err
	}

	dni, err := s.dnrpaClient.GetOwnerDNI(ctx, normalized)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrOwnerLookupUnavailable, err)
	}
	defer secure.Wipe(dni)

	dniEnc, err := s.dniEncryptor.Encrypt(dni)
	if err != nil {
		return fmt.Errorf("no se pudo cifrar el dni para identity: %w", err)
	}

	match, err := s.ownershipVerifier.VerifyOwnership(ctx, input.OwnerID, dniEnc)
	if err != nil {
		return err
	}
	if !match {
		return domain.ErrOwnershipMismatch
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
