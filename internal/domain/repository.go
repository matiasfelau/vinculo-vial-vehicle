package domain

import (
	"context"

	"github.com/google/uuid"
)

// VehicleRepository define la persistencia de vehículos requerida por el dominio.
// La violación de la restricción de unicidad (owner_id, pat_hashed) debe traducirse
// por la implementación concreta en ErrDuplicateVehicle.
type VehicleRepository interface {
	Create(ctx context.Context, vehicle *Vehicle) error

	// FindOwnersByPatHash retorna los owner_id de todos los vehículos vinculados
	// con el Blind Index dado (relación 1 patente -> N dueños). Un slice vacío
	// es un resultado válido, no un error (US-05, /internal/resolve-owner).
	FindOwnersByPatHash(ctx context.Context, patHashed string) ([]uuid.UUID, error)
}
