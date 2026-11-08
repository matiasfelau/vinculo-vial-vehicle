package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidMessage marca un mensaje que nunca va a poder procesarse:
// deserialización fallida, esquema inválido o violación de una regla de
// validación. Es un error permanente: reintentar no cambia el resultado,
// así que el mensaje va directo a la DLQ sin pasar por la cola de retry.
// Los procesadores lo envuelven (fmt.Errorf("...: %w", ErrInvalidMessage))
// cuando el payload no cumple el contrato.
var ErrInvalidMessage = errors.New("mensaje inválido")

// InboundEnvelope es el Event Envelope tal como lo recibe un consumidor.
// Actor es nil cuando el productor lo publicó como null.
type InboundEnvelope struct {
	EventID   uuid.UUID       `json:"eventId"`
	EventType string          `json:"eventType"`
	Timestamp time.Time       `json:"timestamp"`
	Producer  string          `json:"producer"`
	Actor     *string         `json:"actor"`
	Payload   json.RawMessage `json:"payload"`
}

// ParseEnvelope decodifica el cuerpo de un delivery y valida el mínimo
// indispensable para procesarlo. Todo error que devuelve envuelve
// ErrInvalidMessage y ninguno refleja el contenido del mensaje.
func ParseEnvelope(body []byte) (InboundEnvelope, error) {
	var envelope InboundEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return InboundEnvelope{}, invalid("envelope ilegible")
	}
	if envelope.EventID == uuid.Nil {
		return InboundEnvelope{}, invalid("envelope sin eventId")
	}
	if envelope.EventType == "" {
		return InboundEnvelope{}, invalid("envelope sin eventType")
	}
	if envelope.Timestamp.IsZero() {
		return InboundEnvelope{}, invalid("envelope sin timestamp")
	}
	if len(envelope.Payload) == 0 {
		return InboundEnvelope{}, invalid("envelope sin payload")
	}
	return envelope, nil
}

// invalid construye un error permanente cuyo texto describe qué falló del
// contrato sin reproducir el contenido del mensaje.
func invalid(reason string) error {
	return fmt.Errorf("%s: %w", reason, ErrInvalidMessage)
}
