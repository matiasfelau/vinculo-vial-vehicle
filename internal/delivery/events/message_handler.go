package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// deadLetterPublisher envía un mensaje directamente a la DLQ de la
// plataforma, sorteando el ciclo de reintentos. La implementación real
// publica sobre el canal AMQP; en las pruebas se reemplaza por un doble.
type deadLetterPublisher interface {
	PublishToDLQ(ctx context.Context, workQueue string, body []byte, reason string) error
}

// Outcome indica al Consumer qué acción tomar sobre el delivery original.
type Outcome int

const (
	// OutcomeAck confirma el mensaje: se procesó con éxito, era un
	// duplicado ya conocido, o era inválido y la DLQ ya lo confirmó.
	OutcomeAck Outcome = iota
	// OutcomeNackNoRequeue rechaza el mensaje sin reencolarlo de inmediato:
	// la topología de Core lo deriva a la cola .retry, que lo devuelve a la
	// cola de trabajo cuando vence su TTL.
	OutcomeNackNoRequeue
)

// Processor aplica la lógica de negocio sobre un evento cuyo envelope ya se
// validó. Debe ser idempotente: la entrega es at-least-once. El error que
// devuelve se clasifica así:
//   - envuelve ErrInvalidMessage: permanente, va directo a la DLQ;
//   - cualquier otro: transitorio (base o broker caídos, circuito abierto),
//     se reintenta hasta MAX_RETRIES.
type Processor interface {
	Process(ctx context.Context, envelope InboundEnvelope) error
}

// MessageHandler decide, a partir del cuerpo crudo de un delivery, qué
// corresponde hacer con él: validar el envelope, delegar en el Processor y
// clasificar el resultado. No conoce el canal AMQP: esa responsabilidad es
// del Consumer.
type MessageHandler struct {
	processor   Processor
	deadLetters deadLetterPublisher
	workQueue   string
	maxRetries  int64
}

// NewMessageHandler construye el handler de la cola de trabajo workQueue.
// maxRetries es la cantidad de reintentos tras los cuales un fallo
// transitorio se deriva a la DLQ.
func NewMessageHandler(processor Processor, deadLetters deadLetterPublisher, workQueue string, maxRetries int64) *MessageHandler {
	return &MessageHandler{processor: processor, deadLetters: deadLetters, workQueue: workQueue, maxRetries: maxRetries}
}

// Handle procesa un delivery. retries es la cantidad de veces que el mensaje
// ya fue rechazado desde la cola de trabajo (leída del header x-death por el
// Consumer): el corte del ciclo de reintentos lo aplica este handler, no la
// topología.
func (h *MessageHandler) Handle(ctx context.Context, body []byte, retries int64) Outcome {
	envelope, err := ParseEnvelope(body)
	if err != nil {
		return h.sendToDeadLetter(ctx, body, err)
	}

	err = h.safeProcess(ctx, envelope)
	switch {
	case err == nil:
		return OutcomeAck
	case errors.Is(err, ErrInvalidMessage):
		slog.Warn("consumidor: mensaje inválido, se descarta a la dlq", "queue", h.workQueue, "event_id", envelope.EventID, "error", err.Error())
		return h.sendToDeadLetter(ctx, body, err)
	case errors.Is(err, errHandlerPanic):
		return h.sendToDeadLetter(ctx, body, err)
	case retries >= h.maxRetries:
		slog.Error("consumidor: se superó MAX_RETRIES, se descarta a la dlq", "queue", h.workQueue, "event_id", envelope.EventID, "max_retries", h.maxRetries, "error", err.Error())
		return h.sendToDeadLetter(ctx, body, err)
	default:
		slog.Warn("consumidor: fallo transitorio, se reintenta", "queue", h.workQueue, "event_id", envelope.EventID, "intento", retries+1, "max_retries", h.maxRetries, "error", err.Error())
		return OutcomeNackNoRequeue
	}
}

// errHandlerPanic marca un panic capturado en el Processor. Es permanente:
// repetir el mismo mensaje volvería a hacer entrar en pánico al handler.
var errHandlerPanic = errors.New("panic en el procesador del consumidor")

// safeProcess ejecuta el Processor capturando cualquier panic: un panic en
// el handler nunca debe terminar el proceso consumidor ni dejar el mensaje
// en un bucle de reentrega.
func (h *MessageHandler) safeProcess(ctx context.Context, envelope InboundEnvelope) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("consumidor: panic en el procesador", "queue", h.workQueue, "event_id", envelope.EventID, "panic", fmt.Sprint(recovered))
			err = errHandlerPanic
		}
	}()
	return h.processor.Process(ctx, envelope)
}

// sendToDeadLetter publica el mensaje en la DLQ y solo hace ack del
// original si la DLQ confirmó la recepción. Si la publicación falla (broker
// caído, DLQ sin confirmar) el mensaje se rechaza sin requeue para que el
// ciclo normal de reintentos le dé otra oportunidad a la DLQ de estar
// disponible, en lugar de perderlo con un ack que no garantiza nada.
func (h *MessageHandler) sendToDeadLetter(ctx context.Context, body []byte, cause error) Outcome {
	if err := h.deadLetters.PublishToDLQ(ctx, h.workQueue, body, cause.Error()); err != nil {
		slog.Error("consumidor: no se pudo publicar a la dlq, se reintenta", "queue", h.workQueue, "error", err.Error())
		return OutcomeNackNoRequeue
	}
	slog.Warn("consumidor: mensaje derivado a la dlq", "queue", h.workQueue, "reason", cause.Error())
	return OutcomeAck
}
