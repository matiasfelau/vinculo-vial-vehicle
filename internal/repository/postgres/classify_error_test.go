package postgres

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeNetError implementa net.Error para probar classifyError sin depender de
// una conexión de red real.
type fakeNetError struct{}

func (fakeNetError) Error() string   { return "conexión rechazada (prueba)" }
func (fakeNetError) Timeout() bool   { return true }
func (fakeNetError) Temporary() bool { return true }

var _ net.Error = fakeNetError{}

func TestClassifyErrorNilRetornaNil(t *testing.T) {
	if err := classifyError(nil); err != nil {
		t.Fatalf("esperaba nil, obtuvo %v", err)
	}
}

func TestClassifyErrorContextDeadlineExceededEsIndisponibilidad(t *testing.T) {
	err := classifyError(context.DeadlineExceeded)
	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestClassifyErrorContextCanceledEsIndisponibilidad(t *testing.T) {
	err := classifyError(context.Canceled)
	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestClassifyErrorNetErrorEsIndisponibilidad(t *testing.T) {
	err := classifyError(fakeNetError{})
	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestClassifyErrorOtroErrorNoSeReclasifica(t *testing.T) {
	original := errors.New("fallo de sintaxis SQL (prueba)")
	err := classifyError(original)
	if errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatal("un error que no es de conectividad no debe clasificarse como ErrRepositoryUnavailable")
	}
	if !errors.Is(err, original) {
		t.Fatal("el error original debe preservarse cuando no se reclasifica")
	}
}

func TestClassifyErrorPgErrorSQLState(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"08xxx (Connection Exception)", "08001", true},
		{"53xxx (Insufficient Resources)", "53300", true},
		{"57014 (Query Canceled)", "57014", true},
		{"57P01 (Admin Shutdown)", "57P01", true},
		{"57P02 (Crash Shutdown)", "57P02", true},
		{"57000 (Operator Intervention, no match)", "57000", false},
		{"23505 (Unique Violation)", "23505", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// github.com/jackc/pgx/v5/pgconn.PgError
			err := classifyError(&pgconn.PgError{Code: tt.code})
			got := errors.Is(err, domain.ErrRepositoryUnavailable)
			if got != tt.want {
				t.Errorf("classifyError(PgError{Code: %q}): obtuvo %v, esperaba %v", tt.code, got, tt.want)
			}
		})
	}
}
