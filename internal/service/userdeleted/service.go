// Package userdeleted implementa la reacción de Vehicle a la baja de una cuenta
// (US-11).
package userdeleted

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// vehicleRemover es la persistencia que necesita el caso de uso.
type vehicleRemover interface {
	// DeleteByOwner borra todos los vehículos del titular y retorna cuántos borró.
	DeleteByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// RemoveVehiclesService borra los vehículos de un usuario dado de baja.
type RemoveVehiclesService struct {
	vehicles vehicleRemover
}

// NewRemoveVehiclesService construye el caso de uso sobre la persistencia.
func NewRemoveVehiclesService(vehicles vehicleRemover) *RemoveVehiclesService {
	return &RemoveVehiclesService{vehicles: vehicles}
}

// Execute borra los vehículos del titular y retorna cuántos borró. Es
// idempotente: si ya no tiene ninguno (porque el evento se entregó de nuevo, o
// nunca vinculó uno) retorna 0 sin error. Los vínculos de otros titulares con
// las mismas patentes y las patentes pendientes de entrega no se tocan.
func (s *RemoveVehiclesService) Execute(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	removed, err := s.vehicles.DeleteByOwner(ctx, ownerID)
	if err != nil {
		return 0, fmt.Errorf("no se pudieron borrar los vehículos del titular: %w", err)
	}
	return removed, nil
}
