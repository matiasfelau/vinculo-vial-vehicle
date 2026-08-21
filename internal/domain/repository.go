package domain

import "context"

// VehicleRepository define la persistencia de vehículos requerida por el dominio.
// La violación de la restricción de unicidad (owner_id, pat_hashed) debe traducirse
// por la implementación concreta en ErrDuplicateVehicle.
type VehicleRepository interface {
	Create(ctx context.Context, vehicle *Vehicle) error
}
