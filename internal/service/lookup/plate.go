// Package lookup implementa los casos de uso de solo lectura de US-05:
// verificar si una patente está en la red (CheckVehicleService) y resolver
// los dueños de una patente para Alert (ResolveOwnersService).
package lookup

import (
	"fmt"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/secure"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/validation"
)

// PlateDecryptor desencripta pat_enc con la clave privada HPKE de este servicio.
type PlateDecryptor interface {
	Decrypt(ciphertextBase64 string) ([]byte, error)
}

// resolvePatHash desencripta, normaliza, valida y calcula el Blind Index de
// pat_enc: la secuencia común a CheckVehicleService y ResolveOwnersService.
// La patente en claro y su forma normalizada se purgan antes de retornar, en
// todos los caminos, incluidos los de error.
func resolvePatHash(decryptor PlateDecryptor, hmacSecret []byte, patEnc string) (string, error) {
	plate, err := decryptor.Decrypt(patEnc)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrInvalidCryptogram, err)
	}
	defer secure.Wipe(plate)

	normalized := validation.NormalizePlate(plate)
	defer secure.Wipe(normalized)

	if err := validation.ValidatePlate(normalized); err != nil {
		return "", err
	}

	return blindindex.Compute(hmacSecret, normalized), nil
}
