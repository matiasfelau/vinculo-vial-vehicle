// Package events implementa la publicación de eventos de dominio sobre
// RabbitMQ. La topología (exchanges, colas, retry, DLQ) la declara solo
// vinculo-vial-core; este servicio publica sobre topología existente.
package events

import "encoding/json"

// EventEnvelope es el sobre estándar de eventos de la plataforma, obligatorio
// para todos los mensajes publicados por los servicios de Vínculo Vial. Actor es
// nil (JSON null) cuando el evento no lleva identificadores de usuario.
type EventEnvelope struct {
	EventID   string          `json:"eventId"`
	EventType string          `json:"eventType"`
	Timestamp string          `json:"timestamp"`
	Producer  string          `json:"producer"`
	Actor     *string         `json:"actor"`
	Payload   json.RawMessage `json:"payload"`
}
