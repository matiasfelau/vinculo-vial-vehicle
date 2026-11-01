// Package lookup implementa el caso de uso de solo lectura de US-05: resolver
// los dueños de una patente para Alert (ResolveOwnersService).
package lookup

import (
	"fmt"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/secure"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/validation"
)

// PlateDecryptor desencripta pat_enc con la clave privada HPKE de este servicio.
type PlateDecryptor interface {
	Decrypt(ciphertextBase64 string) ([]byte, error)
}

// Verificación en tiempo de compilación de que la implementación concreta de
// producción satisface la interfaz requerida por el caso de uso.
var _ PlateDecryptor = (*hpke.Decryptor)(nil)

// canonicalPlate desencripta pat_enc y retorna la patente canónica: normalizada y
// con formato válido. Es la forma única que usan el Blind Index y el
// recifrado hacia otros servicios. La patente en claro recién desencriptada se
// purga antes de retornar, en todos los caminos; la canónica la purga el llamador
// (secure.Wipe) cuando termina de usarla. Ante error no retorna ninguna patente.
func canonicalPlate(decryptor PlateDecryptor, patEnc string) ([]byte, error) {
	plate, err := decryptor.Decrypt(patEnc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(plate)

	normalized := validation.NormalizePlate(plate)
	if err := validation.ValidatePlate(normalized); err != nil {
		secure.Wipe(normalized)
		return nil, err
	}

	return normalized, nil
}
