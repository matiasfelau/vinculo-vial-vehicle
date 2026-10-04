package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/breaker"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// fakeVerifier es un domain.OwnershipVerifier de prueba que retorna
// exactamente lo configurado y cuenta cuántas veces fue invocado.
type fakeVerifier struct {
	match        bool
	err          error
	invocaciones int
}

func (f *fakeVerifier) VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error) {
	f.invocaciones++
	return f.match, f.err
}

func TestBreakerVerifierDelegaEnExito(t *testing.T) {
	fv := &fakeVerifier{match: true}
	cb := breaker.New(3, time.Minute, time.Second)
	bv := NewBreakerVerifier(fv, cb)

	match, err := bv.VerifyOwnership(context.Background(), uuid.New(), []byte("dni-enc"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !match {
		t.Fatal("esperaba match: true")
	}
	if fv.invocaciones != 1 {
		t.Fatalf("esperaba 1 invocación al verificador decorado, obtuvo %d", fv.invocaciones)
	}
}

func TestBreakerVerifierAbreElCircuitoTrasElUmbralDeFallasContabilizables(t *testing.T) {
	fv := &fakeVerifier{err: domain.ErrOwnershipUnavailable}
	cb := breaker.New(2, time.Minute, time.Minute)
	bv := NewBreakerVerifier(fv, cb)

	for i := 0; i < 2; i++ {
		if _, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil); !errors.Is(err, domain.ErrOwnershipUnavailable) {
			t.Fatalf("llamada #%d: esperaba ErrOwnershipUnavailable, obtuvo %v", i+1, err)
		}
	}

	// Tercera llamada: el circuito ya debe estar abierto y no debe alcanzar al verificador.
	_, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil)
	if !errors.Is(err, domain.ErrOwnershipCircuitOpen) {
		t.Fatalf("esperaba ErrOwnershipCircuitOpen con el circuito abierto, obtuvo %v", err)
	}
	if fv.invocaciones != 2 {
		t.Fatalf("con el circuito abierto no debía invocarse el verificador decorado; invocaciones=%d", fv.invocaciones)
	}
}

func TestBreakerVerifierUn4xxNoAbreElCircuito(t *testing.T) {
	// Un 4xx del cliente real no se envuelve en ErrOwnershipUnavailable ni en
	// ErrOwnershipUpstreamFailure: se simula acá con un error genérico equivalente.
	fv := &fakeVerifier{err: errors.New("domain: identity rechazó la solicitud con estado 400")}
	cb := breaker.New(1, time.Minute, time.Minute)
	bv := NewBreakerVerifier(fv, cb)

	for i := 0; i < 5; i++ {
		if _, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil); err == nil {
			t.Fatalf("llamada #%d: esperaba que el error del verificador se propague", i+1)
		}
	}

	if fv.invocaciones != 5 {
		t.Fatalf("un 4xx repetido no debe abrir el circuito; invocaciones esperadas=5, obtuvo=%d", fv.invocaciones)
	}
}

func TestBreakerVerifierUpstreamFailureCuentaComoFalla(t *testing.T) {
	fv := &fakeVerifier{err: domain.ErrOwnershipUpstreamFailure}
	cb := breaker.New(1, time.Minute, time.Minute)
	bv := NewBreakerVerifier(fv, cb)

	if _, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil); !errors.Is(err, domain.ErrOwnershipUpstreamFailure) {
		t.Fatalf("esperaba ErrOwnershipUpstreamFailure, obtuvo %v", err)
	}

	_, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil)
	if !errors.Is(err, domain.ErrOwnershipCircuitOpen) {
		t.Fatalf("esperaba el circuito abierto tras un ErrOwnershipUpstreamFailure con umbral 1, obtuvo %v", err)
	}
	if fv.invocaciones != 1 {
		t.Fatalf("con el circuito abierto no debía invocarse el verificador decorado; invocaciones=%d", fv.invocaciones)
	}
}

func TestBreakerVerifierUnExitoTrasFallasCierraElCircuito(t *testing.T) {
	fv := &fakeVerifier{err: domain.ErrOwnershipUnavailable}
	cb := breaker.New(2, time.Minute, time.Minute)
	bv := NewBreakerVerifier(fv, cb)

	bv.VerifyOwnership(context.Background(), uuid.New(), nil)

	fv.err = nil
	fv.match = true
	if _, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil); err != nil {
		t.Fatalf("no se esperaba error en la llamada exitosa: %v", err)
	}

	fv.err = domain.ErrOwnershipUnavailable
	if _, err := bv.VerifyOwnership(context.Background(), uuid.New(), nil); !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba que el circuito siguiera cerrado tras el éxito, obtuvo %v", err)
	}
}

func TestBreakerVerifierCircuitStateReflejaElBreaker(t *testing.T) {
	fv := &fakeVerifier{err: domain.ErrOwnershipUnavailable}
	cb := breaker.New(1, time.Minute, time.Minute)
	bv := NewBreakerVerifier(fv, cb)

	if got := bv.CircuitState(); got != "closed" {
		t.Fatalf("esperaba \"closed\" antes de fallar, obtuvo %q", got)
	}

	bv.VerifyOwnership(context.Background(), uuid.New(), nil)

	if got := bv.CircuitState(); got != "open" {
		t.Fatalf("esperaba \"open\" tras alcanzar el umbral, obtuvo %q", got)
	}
}
