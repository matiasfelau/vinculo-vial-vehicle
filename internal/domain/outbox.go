package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OutboxEvent es un evento de dominio pendiente de publicar, persistido en la
// misma transacción que el cambio de estado que lo origina. La publicación la
// hace un worker en background, para no acoplar la operación de negocio a la
// disponibilidad del broker: la tabla de outbox es la única fuente de verdad de
// "qué falta publicar".
//
// El outbox es genérico: EventType es el eventType del envelope y también la
// routing key, Actor es el campo actor del envelope (nil cuando el evento no
// lleva identificadores de usuario) y Payload es el JSON ya serializado.
type OutboxEvent struct {
	ID        uuid.UUID
	EventType string
	Actor     *uuid.UUID
	Payload   json.RawMessage
	CreatedAt time.Time
}

// EventPublisher publica eventos de dominio hacia el message broker. El
// OutboxEvent.ID se reutiliza como eventId del envelope: si una publicación se
// reintenta (por ejemplo, se confirmó pero no se pudo marcar como enviada), el
// consumidor que deduplique por eventId la trata como el mismo evento.
type EventPublisher interface {
	Publish(ctx context.Context, event OutboxEvent) error
}

// OutboxRepository expone las operaciones que el worker de publicación necesita:
// traer eventos pendientes y registrar el resultado del intento de publicarlos.
type OutboxRepository interface {
	// FetchPending devuelve hasta limit eventos todavía no enviados, ordenados
	// por antigüedad.
	FetchPending(ctx context.Context, limit int) ([]OutboxEvent, error)
	// MarkSent registra que el evento se publicó y el broker lo confirmó.
	MarkSent(ctx context.Context, id uuid.UUID) error
	// MarkFailed registra un intento fallido sin cambiar el estado pendiente: el
	// worker lo reintenta en el próximo ciclo.
	MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error
}

// EventTypePendingClaimed es el eventType (y routing key) del evento que avisa
// a Alert de que el primer dueño de una patente con alertas pendientes confirmó
// su titularidad.
const EventTypePendingClaimed = "vehicle.pending.claimed"

// PendingClaimedPayload es el payload de vehicle.pending.claimed. Lleva solo el
// pending_ref: ni la patente ni el blind index viajan por el bus. El dueño va en
// el actor del envelope.
type PendingClaimedPayload struct {
	PendingRef uuid.UUID `json:"pending_ref"`
}

// NewPendingClaimedEvent construye el evento de outbox vehicle.pending.claimed.
// El dueño que vinculó el vehículo va en Actor, nunca en el payload.
func NewPendingClaimedEvent(id, ownerID, pendingRef uuid.UUID, createdAt time.Time) (OutboxEvent, error) {
	payload, err := json.Marshal(PendingClaimedPayload{PendingRef: pendingRef})
	if err != nil {
		return OutboxEvent{}, err
	}
	return OutboxEvent{
		ID:        id,
		EventType: EventTypePendingClaimed,
		Actor:     &ownerID,
		Payload:   payload,
		CreatedAt: createdAt,
	}, nil
}
