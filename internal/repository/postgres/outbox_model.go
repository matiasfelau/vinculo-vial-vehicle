package postgres

import (
	"time"

	"github.com/google/uuid"
)

const (
	outboxStatusPending = "pending"
	outboxStatusSent    = "sent"
)

// outboxEventModel es la representación GORM de la tabla outbox_events, el
// outbox genérico del servicio: event_type es el eventType y la routing key,
// actor es el campo actor del envelope (null si el evento no lleva
// identificadores de usuario) y payload es el JSON del payload.
type outboxEventModel struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	EventType string     `gorm:"column:event_type;type:varchar(64);not null"`
	Actor     *uuid.UUID `gorm:"type:uuid"`
	Payload   string     `gorm:"type:jsonb;not null"`
	Status    string     `gorm:"type:varchar(16);not null;default:pending;index:idx_outbox_events_status_created_at,priority:1"`
	Attempts  int        `gorm:"not null;default:0"`
	LastError *string    `gorm:"column:last_error;type:text"`
	CreatedAt time.Time  `gorm:"type:timestamptz;not null;index:idx_outbox_events_status_created_at,priority:2"`
	SentAt    *time.Time `gorm:"column:sent_at;type:timestamptz"`
}

func (outboxEventModel) TableName() string {
	return "outbox_events"
}
