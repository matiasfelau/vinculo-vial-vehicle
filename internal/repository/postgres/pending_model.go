package postgres

import (
	"time"

	"github.com/google/uuid"
)

// pendingPlateModel es la representación GORM de la tabla pending_plates: las
// patentes sin dueños que tienen alertas esperando entrega (US-10). plate_hmac
// es el mismo Blind Index (HMAC-SHA256) de vehicles.pat_hashed y es único: una
// patente tiene a lo sumo una fila. Nunca guarda la patente en claro, ni
// identificadores de alertas ni de emisores.
type pendingPlateModel struct {
	PlateHMAC  string    `gorm:"column:plate_hmac;primaryKey"`
	PendingRef uuid.UUID `gorm:"column:pending_ref;type:uuid;not null;uniqueIndex"`
	ExpiresAt  time.Time `gorm:"column:expires_at;type:timestamptz;not null;index"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (pendingPlateModel) TableName() string {
	return "pending_plates"
}
