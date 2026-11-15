// Package vehicle implementa los casos de uso de vinculación de vehículos por
// dominio (US-14): vincular, contar, desvincular una patente y desvincular todas.
// La patente la escribe el usuario y no se verifica su titularidad.
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
	_ PlateCipher      = (*symmetric.Cipher)(nil)
)

// PayloadDecryptor desencripta payload_enc con la clave privada HPKE de este servicio.
type PayloadDecryptor interface {
	Decrypt(ciphertextBase64 string) ([]byte, error)
}

// PlateCipher cifra la patente en claro para su persistencia at-rest.
type PlateCipher interface {
	Encrypt(plaintext []byte) (string, error)
}

// PlateInput agrupa los datos de las operaciones que reciben una patente
// cifrada: vincular y desvincular una.
type PlateInput struct {
	OwnerID    uuid.UUID
	PayloadEnc string
}

// Summary es lo único que Vehicle cuenta de los vínculos de un usuario: cuántos
// tiene y cuántos puede tener. Nunca incluye patentes.
type Summary struct {
	Count int `json:"count"`
	Max   int `json:"max"`
}

// vehiclePayload es el contenido de payload_enc una vez desencriptado. Patente es
// []byte (no string) para preservar Zero-Retention: encoding/json decodifica el
// base64 directamente a bytes, sin materializar la patente como string de Go.
// Cualquier otro campo, como el chasis que enviaba la app anterior, se ignora.
type vehiclePayload struct {
	Patente []byte `json:"patente"`
}

// Service orquesta los casos de uso de vinculación por dominio.
type Service struct {
	repository       domain.VehicleRepository
	payloadDecryptor PayloadDecryptor
	plateCipher      PlateCipher
	hmacSecret       []byte
	maxPerUser       int
}

// NewService construye el servicio. maxPerUser es VEHICLE_MAX_PER_USER.
func NewService(
	repository domain.VehicleRepository,
	payloadDecryptor PayloadDecryptor,
	plateCipher PlateCipher,
	hmacSecret []byte,
	maxPerUser int,
) *Service {
	return &Service{
		repository:       repository,
		payloadDecryptor: payloadDecryptor,
		plateCipher:      plateCipher,
		hmacSecret:       hmacSecret,
		maxPerUser:       maxPerUser,
	}
}

// plateFromPayload desencripta payload_enc y retorna la patente normalizada y
// validada. El llamador debe purgarla con secure.Wipe. Los buffers intermedios
// se purgan acá.
func (s *Service) plateFromPayload(payloadEnc string) ([]byte, error) {
	payload, err := s.payloadDecryptor.Decrypt(payloadEnc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(payload)

	var data vehiclePayload
	if err := json.Unmarshal(payload, &data); err != nil {
		// El detalle del error de json puede citar un carácter del payload: no se propaga.
		return nil, fmt.Errorf("%w: el payload no tiene el formato esperado", domain.ErrInvalidCryptogram)
	}
	defer secure.Wipe(data.Patente)

	normalized := validation.NormalizePlate(data.Patente)
	if err := validation.ValidatePlate(normalized); err != nil {
		secure.Wipe(normalized)
		return nil, err
	}
	return normalized, nil
}

// Register vincula la patente al usuario de la sesión: blind index y AES de la
// patente, tope por usuario y reclamo de alertas pendientes. Retorna cuántos
// vehículos tiene el usuario después del alta.
func (s *Service) Register(ctx context.Context, input PlateInput) (Summary, error) {
	normalized, err := s.plateFromPayload(input.PayloadEnc)
	if err != nil {
		return Summary{}, err
	}
	defer secure.Wipe(normalized)

	patHashed := blindindex.Compute(s.hmacSecret, normalized)

	patAES, err := s.plateCipher.Encrypt(normalized)
	if err != nil {
		return Summary{}, fmt.Errorf("no se pudo cifrar la patente: %w", err)
	}

	count, err := s.repository.Create(ctx, &domain.Vehicle{
		ID:        uuid.New(),
		OwnerID:   input.OwnerID,
		PatHashed: patHashed,
		PatAES:    patAES,
		CreatedAt: time.Now().UTC(),
	}, s.maxPerUser)
	if err != nil {
		return Summary{}, err
	}
	return Summary{Count: count, Max: s.maxPerUser}, nil
}

// Unlink desvincula la patente escrita de entre los vehículos del usuario de la
// sesión y libera el lugar. Retorna ErrVehicleNotFound si no la tiene vinculada,
// aunque la tenga otro usuario.
func (s *Service) Unlink(ctx context.Context, input PlateInput) (Summary, error) {
	normalized, err := s.plateFromPayload(input.PayloadEnc)
	if err != nil {
		return Summary{}, err
	}
	defer secure.Wipe(normalized)

	remaining, err := s.repository.DeleteByOwnerAndPlate(ctx, input.OwnerID, blindindex.Compute(s.hmacSecret, normalized))
	if err != nil {
		return Summary{}, err
	}
	return Summary{Count: remaining, Max: s.maxPerUser}, nil
}

// UnlinkAll desvincula todos los vehículos del usuario. Es idempotente.
func (s *Service) UnlinkAll(ctx context.Context, ownerID uuid.UUID) error {
	_, err := s.repository.DeleteByOwner(ctx, ownerID)
	return err
}

// Count retorna cuántos vehículos tiene vinculados el usuario y el máximo.
func (s *Service) Count(ctx context.Context, ownerID uuid.UUID) (Summary, error) {
	count, err := s.repository.CountByOwner(ctx, ownerID)
	if err != nil {
		return Summary{}, err
	}
	return Summary{Count: count, Max: s.maxPerUser}, nil
}
