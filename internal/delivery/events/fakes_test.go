package events

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

const testQueue = "vehicle.user-deleted.q"

// fakeDeferredConfirmation implementa deferredConfirmation con un resultado
// configurable. Si block es true espera a que el contexto venza, para
// simular un broker que no confirma.
type fakeDeferredConfirmation struct {
	acked bool
	err   error
	block bool
}

func (f fakeDeferredConfirmation) WaitContext(ctx context.Context) (bool, error) {
	if f.block {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return f.acked, f.err
}

type fakeQosCall struct {
	prefetchCount int
	prefetchSize  int
	global        bool
}

// fakeConsumerChannel implementa ConsumerChannel registrando las llamadas
// recibidas y devolviendo resultados configurables.
type fakeConsumerChannel struct {
	qosCalls []fakeQosCall
	qosErr   error

	confirmCalls int
	confirmErr   error

	notifyReturnCalls int

	consumeQueue      string
	consumeAutoAck    bool
	consumeDeliveries chan amqp.Delivery
	consumeErr        error

	publishedExchange   string
	publishedKey        string
	publishedMandatory  bool
	publishedImmediate  bool
	publishedMsg        amqp.Publishing
	publishedCtx        context.Context
	publishConfirmation deferredConfirmation
	publishErr          error
}

func (f *fakeConsumerChannel) Qos(prefetchCount, prefetchSize int, global bool) error {
	f.qosCalls = append(f.qosCalls, fakeQosCall{prefetchCount: prefetchCount, prefetchSize: prefetchSize, global: global})
	return f.qosErr
}

func (f *fakeConsumerChannel) Confirm(bool) error {
	f.confirmCalls++
	return f.confirmErr
}

func (f *fakeConsumerChannel) NotifyReturn(c chan amqp.Return) chan amqp.Return {
	f.notifyReturnCalls++
	// Se cierra de inmediato: no hay devoluciones que simular y watchReturns
	// debe terminar sola en lugar de dejar una goroutine bloqueada.
	close(c)
	return c
}

func (f *fakeConsumerChannel) Consume(queue, _ string, autoAck, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	f.consumeQueue = queue
	f.consumeAutoAck = autoAck
	if f.consumeErr != nil {
		return nil, f.consumeErr
	}
	return f.consumeDeliveries, nil
}

func (f *fakeConsumerChannel) PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (deferredConfirmation, error) {
	f.publishedCtx = ctx
	f.publishedExchange = exchange
	f.publishedKey = key
	f.publishedMandatory = mandatory
	f.publishedImmediate = immediate
	f.publishedMsg = msg
	return f.publishConfirmation, f.publishErr
}

func (f *fakeConsumerChannel) Close() error {
	return nil
}

var _ ConsumerChannel = (*fakeConsumerChannel)(nil)

// fakeAcknowledger permite verificar, sin un canal AMQP real, si el
// consumidor confirmó o rechazó un delivery y con qué parámetros.
type fakeAcknowledger struct {
	acked         []uint64
	nacked        []uint64
	nackedRequeue []bool
}

func (f *fakeAcknowledger) Ack(tag uint64, _ bool) error {
	f.acked = append(f.acked, tag)
	return nil
}

func (f *fakeAcknowledger) Nack(tag uint64, _, requeue bool) error {
	f.nacked = append(f.nacked, tag)
	f.nackedRequeue = append(f.nackedRequeue, requeue)
	return nil
}

func (f *fakeAcknowledger) Reject(uint64, bool) error {
	return nil
}

// fakeDeadLetterPublisher registra lo publicado en la DLQ.
type fakeDeadLetterPublisher struct {
	err       error
	published []publishedToDLQ
}

type publishedToDLQ struct {
	queue  string
	body   []byte
	reason string
}

func (f *fakeDeadLetterPublisher) PublishToDLQ(_ context.Context, workQueue string, body []byte, reason string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, publishedToDLQ{queue: workQueue, body: body, reason: reason})
	return nil
}
