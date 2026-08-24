package validation

import (
	"bytes"
	"errors"
	"testing"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func TestNormalizePlate(t *testing.T) {
	casos := []struct {
		entrada  string
		esperado string
	}{
		{"aaa000", "AAA000"},
		{"AA-000-AA", "AA000AA"},
		{" ab 123 cd ", "AB123CD"},
	}

	for _, c := range casos {
		got := NormalizePlate([]byte(c.entrada))
		if !bytes.Equal(got, []byte(c.esperado)) {
			t.Errorf("NormalizePlate(%q) = %q, esperaba %q", c.entrada, got, c.esperado)
		}
	}
}

func TestValidatePlateFormatoViejo(t *testing.T) {
	if err := ValidatePlate([]byte("AAA000")); err != nil {
		t.Errorf("esperaba formato viejo válido, obtuvo error: %v", err)
	}
}

func TestValidatePlateFormatoNuevo(t *testing.T) {
	if err := ValidatePlate([]byte("AA000AA")); err != nil {
		t.Errorf("esperaba formato nuevo válido, obtuvo error: %v", err)
	}
}

func TestValidatePlateInvalida(t *testing.T) {
	invalidas := []string{"", "AAA00", "AAAA000", "AA000A", "12A345", "AA000AAA"}

	for _, p := range invalidas {
		err := ValidatePlate([]byte(p))
		if !errors.Is(err, domain.ErrInvalidPlate) {
			t.Errorf("ValidatePlate(%q): esperaba ErrInvalidPlate, obtuvo %v", p, err)
		}
	}
}
