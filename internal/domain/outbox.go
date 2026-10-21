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
