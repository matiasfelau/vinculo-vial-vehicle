package events

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// deferredConfirmation es el subconjunto de *amqp.DeferredConfirmation que
// este paquete consume. amqp091-go no expone un constructor público para
// ese tipo: sin esta interfaz no habría forma de construir un doble de
// prueba que resuelva WaitContext sin un broker real.
type deferredConfirmation interface {
	WaitContext(ctx context.Context) (bool, error)
}

var _ deferredConfirmation = (*amqp.DeferredConfirmation)(nil)

// consumeChannel es el subconjunto del canal que Consumer necesita.
type consumeChannel interface {
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
}

// publishChannel es el subconjunto del canal que AMQPDeadLetterPublisher
// necesita.
type publishChannel interface {
	PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (deferredConfirmation, error)
}

// ConsumerChannel es el subconjunto de *amqp.Channel que usa la
// infraestructura de consumo: lo que configureChannel aplica al abrir la
// conexión (Qos, Confirm, NotifyReturn) más lo que necesitan Consumer y
// AMQPDeadLetterPublisher. Aislarlo permite testear todo con un doble, sin
// un broker real.
type ConsumerChannel interface {
	consumeChannel
	publishChannel
	Qos(prefetchCount, prefetchSize int, global bool) error
	Confirm(noWait bool) error
	NotifyReturn(c chan amqp.Return) chan amqp.Return
	Close() error
}

// realConsumerChannel adapta *amqp.Channel a ConsumerChannel. El método
// real PublishWithDeferredConfirmWithContext devuelve el tipo concreto
// *amqp.DeferredConfirmation; Go no permite que *amqp.Channel satisfaga una
// interfaz cuyo mismo método devuelve deferredConfirmation, así que este
// adaptador hace la traducción. El resto de los métodos se promueven sin
// cambios desde el canal embebido.
type realConsumerChannel struct {
	*amqp.Channel
}

func (c realConsumerChannel) PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (deferredConfirmation, error) {
	confirmation, err := c.Channel.PublishWithDeferredConfirmWithContext(ctx, exchange, key, mandatory, immediate, msg)
	// Conversión explícita: un *amqp.DeferredConfirmation nil asignado
	// directo a la interfaz produciría una interfaz no-nil que envuelve un
	// puntero nil, y PublishToDLQ dejaría de detectar con == nil el caso
	// documentado por el SDK (canal fuera de modo confirm).
	if confirmation == nil {
		return nil, err
	}
	return confirmation, err
}

var _ ConsumerChannel = realConsumerChannel{}
