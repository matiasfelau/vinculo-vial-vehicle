package postgres

import (
	"time"

	"github.com/google/uuid"
)

// vehicleModel es la representación GORM de la tabla vehicles.
// UNIQUE(owner_id, pat_hashed): un mismo propietario no puede vincular dos veces
// la misma patente; propietarios distintos sí pueden vincular la misma patente.
type vehicleModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	OwnerID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_vehicles_owner_plate"`
	PatHashed string    `gorm:"column:pat_hashed;not null;uniqueIndex:idx_vehicles_owner_plate"`
	PatAES    string    `gorm:"column:pat_aes;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func (vehicleModel) TableName() string {
	return "vehicles"
}
