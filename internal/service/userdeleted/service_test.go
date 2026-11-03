package userdeleted

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeVehicles struct {
	borrados int64
	err      error
	owners   []uuid.UUID
}

func (f *fakeVehicles) DeleteByOwner(_ context.Context, ownerID uuid.UUID) (int64, error) {
	f.owners = append(f.owners, ownerID)
	return f.borrados, f.err
}

func TestExecuteBorraLosVehiculosDelTitularYRetornaCuantosFueron(t *testing.T) {
	ownerID := uuid.New()
	vehicles := &fakeVehicles{borrados: 3}

	removed, err := NewRemoveVehiclesService(vehicles).Execute(context.Background(), ownerID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if removed != 3 {
		t.Errorf("borrados = %d, esperaba 3", removed)
	}
	if len(vehicles.owners) != 1 || vehicles.owners[0] != ownerID {
		t.Errorf("titulares consultados = %v, esperaba solo %v", vehicles.owners, ownerID)
	}
}

// Un titular sin vehículos (porque el evento se reentregó, o nunca vinculó uno)
// no es un error: la baja es idempotente.
func TestExecuteSinVehiculosRetornaCeroSinError(t *testing.T) {
	removed, err := NewRemoveVehiclesService(&fakeVehicles{}).Execute(context.Background(), uuid.New())

	if err != nil || removed != 0 {
		t.Fatalf("borrados = %d, error = %v; esperaba 0 sin error", removed, err)
	}
}

func TestExecutePropagaElErrorDeLaPersistencia(t *testing.T) {
	causa := errors.New("base de datos caída")

	removed, err := NewRemoveVehiclesService(&fakeVehicles{err: causa}).Execute(context.Background(), uuid.New())

	if !errors.Is(err, causa) {
		t.Fatalf("error = %v, esperaba que conservara la causa", err)
	}
	if removed != 0 {
		t.Errorf("borrados = %d, esperaba 0 ante un error", removed)
	}
}
