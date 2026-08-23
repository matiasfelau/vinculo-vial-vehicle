// Package validation valida y normaliza el formato de patente argentina.
package validation

import (
	"bytes"
	"regexp"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

var (
	oldFormat = regexp.MustCompile(`^[A-Z]{3}[0-9]{3}$`)
	newFormat = regexp.MustCompile(`^[A-Z]{2}[0-9]{3}[A-Z]{2}$`)
)

// NormalizePlate normaliza la patente a mayúsculas, sin espacios ni guiones.
// Opera exclusivamente sobre []byte para no convertir el dato sensible a string.
func NormalizePlate(plate []byte) []byte {
	upper := bytes.ToUpper(plate)

	normalized := make([]byte, 0, len(upper))
	for _, b := range upper {
		if b == ' ' || b == '-' {
			continue
		}
		normalized = append(normalized, b)
	}

	return normalized
}

// ValidatePlate valida que la patente ya normalizada cumpla el formato argentino
// vigente (AAA000) o el nuevo formato Mercosur (AA000AA).
func ValidatePlate(plate []byte) error {
	if oldFormat.Match(plate) || newFormat.Match(plate) {
		return nil
	}
	return domain.ErrInvalidPlate
}
