package events

import (
	"context"
	"errors"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ErrConnectionLost indica que el canal de deliveries se cerró sin que el
// contexto estuviera cancelado: el broker o la conexión cayeron. Quien
// orquesta el ciclo de vida del consumidor (RunWithReconnect) usa esta
// distinción para decidir si debe reconectar o si el retorno de Run
// responde a un apagado pedido.
var ErrConnectionLost = errors.New("se perdió la conexión con rabbitmq")

// messageHandler decide qué hacer con el cuerpo crudo de un delivery.
type messageHandler interface {
	Handle(ctx context.Context, body []byte, retries int64) Outcome
}

// Consumer consume una cola de trabajo y aplica la decisión del handler
// sobre cada delivery. El ack es siempre manual: la cola se consume con
// autoAck en false.
type Consumer struct {
	channel consumeChannel
	queue   string
	handler messageHandler
}

// NewConsumer construye un Consumer sobre la cola de trabajo queue.
func NewConsumer(channel consumeChannel, queue string, handler messageHandler) *Consumer {
	return &Consumer{channel: channel, queue: queue, handler: handler}
}

// Run consume la cola hasta que el contexto se cancela o el canal se cierra.
// Devuelve cuando termina de procesar el mensaje en vuelo, lo que le
// permite al llamador coordinar un apagado ordenado. Los mensajes que el
// broker ya había entregado pero no se llegaron a procesar quedan sin ack:
// al cerrarse el canal vuelven a la cola de trabajo.
func (c *Consumer) Run(ctx context.Context) error {
	deliveries, err := c.channel.Consume(c.queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return ErrConnectionLost
			}
			c.process(ctx, delivery)
		}
	}
}

// process procesa un delivery. El apagado no cancela el mensaje en vuelo: con
// el contexto cancelado, la consulta a la base fallaría y el mensaje sería
// clasificado como fallo transitorio, gastando un reintento (o, con
// MAX_RETRIES=0, yendo a la DLQ) por un simple despliegue. Terminarlo está
// acotado por los plazos propios de la consulta y de la DLQ.
func (c *Consumer) process(ctx context.Context, delivery amqp.Delivery) {
	retries := deathCount(delivery.Headers, c.queue)

	switch c.handler.Handle(context.WithoutCancel(ctx), delivery.Body, retries) {
	case OutcomeAck:
		if err := delivery.Ack(false); err != nil {
			slog.Error("consumidor: no se pudo confirmar el mensaje", "queue", c.queue, "error", err.Error())
		}
	case OutcomeNackNoRequeue:
		if err := delivery.Nack(false, false); err != nil {
			slog.Error("consumidor: no se pudo rechazar el mensaje", "queue", c.queue, "error", err.Error())
		}
	}
}
