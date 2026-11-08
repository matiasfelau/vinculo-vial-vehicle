package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// eventTypeUserDeleted es el eventType (y routing key) con el que Identity
// informa la baja de una cuenta.
const eventTypeUserDeleted = "user.deleted"

// userVehiclesRemover borra los vehículos de un usuario dado de baja y
// retorna cuántos borró.
type userVehiclesRemover interface {
	Execute(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// UserDeletedProcessor es el events.Processor de user.deleted (US-11): valida
// el envelope y el payload contra el contrato y delega el borrado de los
// vehículos del usuario en el caso de uso. Todo mensaje que no cumple el
// contrato es inválido (va a la DLQ sin reintento); cualquier otro error del
// caso de uso es transitorio.
type UserDeletedProcessor struct {
	remover userVehiclesRemover
}

var _ Processor = (*UserDeletedProcessor)(nil)

// NewUserDeletedProcessor construye el procesador sobre el caso de uso.
func NewUserDeletedProcessor(remover userVehiclesRemover) *UserDeletedProcessor {
	return &UserDeletedProcessor{remover: remover}
}

// Process implementa Processor. Es idempotente porque lo es el caso de uso:
// repetir la entrega de un mismo user.deleted no encuentra vehículos que
// borrar. Loguea el eventId y la cantidad, nunca el usuario.
func (p *UserDeletedProcessor) Process(ctx context.Context, envelope InboundEnvelope) error {
	ownerID, err := parseUserDeleted(envelope)
	if err != nil {
		return err
	}

	removed, err := p.remover.Execute(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("user.deleted: %w", err)
	}

	slog.Info("consumidor: vehículos del usuario dado de baja borrados", "event_id", envelope.EventID, "vehiculos_borrados", removed)
	return nil
}

// userDeletedPayload es el payload de user.deleted. Vehicle no usa deleted_at,
// pero el contrato lo exige y un payload sin él no es un user.deleted válido.
type userDeletedPayload struct {
	DeletedAt *string `json:"deleted_at"`
}

// parseUserDeleted valida el envelope y el payload de user.deleted y devuelve
// el usuario dado de baja, que es el actor del evento. Todo error que devuelve
// envuelve ErrInvalidMessage y ninguno refleja el contenido del mensaje.
func parseUserDeleted(envelope InboundEnvelope) (uuid.UUID, error) {
	if envelope.EventType != eventTypeUserDeleted {
		return uuid.Nil, invalid("eventType inesperado")
	}

	if envelope.Actor == nil {
		return uuid.Nil, invalid("actor ausente")
	}
	ownerID, err := uuid.Parse(*envelope.Actor)
	if err != nil || ownerID == uuid.Nil {
		return uuid.Nil, invalid("actor inválido")
	}

	var payload userDeletedPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return uuid.Nil, invalid("payload ilegible")
	}
	if payload.DeletedAt == nil {
		return uuid.Nil, invalid("deleted_at ausente")
	}
	if _, err := time.Parse(time.RFC3339, *payload.DeletedAt); err != nil {
		return uuid.Nil, invalid("deleted_at no es RFC3339")
	}

	return ownerID, nil
}
