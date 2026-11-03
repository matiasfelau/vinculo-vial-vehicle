package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// dlqExchange es el exchange de dead letter de la plataforma. Es un nombre
// fijo de la topología: no varía entre entornos y no se declara desde acá
// (lo hace vinculo-vial-core).
const dlqExchange = "vvi.dlq.exchange"

const headerDiscardReason = "x-discard-reason"

// errDLQNotConfirmed se devuelve cuando el broker no confirmó la
// publicación a la DLQ (nack de publisher confirm) o el canal no está en
// modo confirm. En ambos casos no hay garantía de que el mensaje haya
// quedado persistido: el llamador no debe hacer ack del mensaje original.
var errDLQNotConfirmed = errors.New("la dlq no confirmó la publicación")

// errDLQQueueRequired se devuelve cuando no se informa la cola de origen.
// La DLQ está bindeada por el nombre de la cola de trabajo: publicar con
// otra routing key (o vacía) dejaría el mensaje sin ruta.
var errDLQQueueRequired = errors.New("la cola de origen es obligatoria para publicar en la dlq")

// AMQPDeadLetterPublisher publica directamente al exchange de dead letter,
// sorteando el ciclo de reintentos. No declara exchanges, colas ni
// bindings: solo publica sobre la topología que declara Core.
type AMQPDeadLetterPublisher struct {
	channel publishChannel
	timeout time.Duration
}

// NewAMQPDeadLetterPublisher construye el publisher. timeout acota la
// publicación y la espera del publisher confirm.
func NewAMQPDeadLetterPublisher(channel publishChannel, timeout time.Duration) *AMQPDeadLetterPublisher {
	return &AMQPDeadLetterPublisher{channel: channel, timeout: timeout}
}

var _ deadLetterPublisher = (*AMQPDeadLetterPublisher)(nil)

// PublishToDLQ publica el mensaje en vvi.dlq.exchange con routing key igual
// al nombre de la cola de trabajo de la que proviene (workQueue), que es
// por lo que está bindeada su DLQ. Lo publica persistente y con
// mandatory=true, y espera el publisher confirm antes de devolver éxito. El
// canal debe estar en modo confirm; de no estarlo se trata como fallo en
// lugar de arriesgar un ack del original sin garantía de que la DLQ lo
// recibió.
//
// Un mensaje mandatory que el broker no puede rutear igual recibe un ack de
// confirm (certifica que el broker lo procesó, no que lo ruteó): esa
// situación se reporta aparte vía NotifyReturn (ver watchReturns).
func (p *AMQPDeadLetterPublisher) PublishToDLQ(ctx context.Context, workQueue string, body []byte, reason string) error {
	if workQueue == "" {
		return errDLQQueueRequired
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, dlqExchange, workQueue, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Headers:      amqp.Table{headerDiscardReason: reason},
	})
	if err != nil {
		return fmt.Errorf("no se pudo publicar a la dlq: %w", err)
	}
	if confirmation == nil {
		return errDLQNotConfirmed
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("no se recibió confirmación de la dlq: %w", err)
	}
	if !acked {
		return errDLQNotConfirmed
	}

	return nil
}
