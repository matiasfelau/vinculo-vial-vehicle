package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// handlerFijo devuelve siempre el mismo Outcome y registra lo que recibe. Si
// enCurso no es nil, avisa que empezó a procesar y espera a que lo liberen; en
// ese tiempo observa el contexto, para probar qué recibe un mensaje en vuelo.
type handlerFijo struct {
	outcome    Outcome
	bodies     [][]byte
	retries    []int64
	enCurso    chan struct{}
	liberar    chan struct{}
	ctxAlFinal error
}

func (h *handlerFijo) Handle(ctx context.Context, body []byte, retries int64) Outcome {
	h.bodies = append(h.bodies, body)
	h.retries = append(h.retries, retries)
	if h.enCurso != nil {
		close(h.enCurso)
		<-h.liberar
		h.ctxAlFinal = ctx.Err()
	}
	return h.outcome
}

func TestConsumerProcessHaceAckSiElHandlerLoPide(t *testing.T) {
	handler := &handlerFijo{outcome: OutcomeAck}
	consumer := NewConsumer(nil, testQueue, handler)
	ack := &fakeAcknowledger{}

	consumer.process(context.Background(), amqp.Delivery{Acknowledger: ack, DeliveryTag: 1, Body: []byte(`{}`)})

	if len(ack.acked) != 1 || ack.acked[0] != 1 || len(ack.nacked) != 0 {
		t.Fatalf("esperaba Ack(1), obtuvo acked=%v nacked=%v", ack.acked, ack.nacked)
	}
}

func TestConsumerProcessRechazaSinRequeueSiElHandlerLoPide(t *testing.T) {
	handler := &handlerFijo{outcome: OutcomeNackNoRequeue}
	consumer := NewConsumer(nil, testQueue, handler)
	ack := &fakeAcknowledger{}

	consumer.process(context.Background(), amqp.Delivery{Acknowledger: ack, DeliveryTag: 7, Body: []byte(`{}`)})

	if len(ack.nacked) != 1 || ack.nacked[0] != 7 {
		t.Fatalf("esperaba Nack(7), obtuvo acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	if ack.nackedRequeue[0] {
		t.Fatal("el nack no debe reencolar de inmediato: un requeue inmediato produce un bucle de reentrega en caliente")
	}
	if len(ack.acked) != 0 {
		t.Fatalf("no debía confirmarse el mensaje, acked=%v", ack.acked)
	}
}

func TestConsumerProcessLeeXDeathSoloDeLaColaDeTrabajo(t *testing.T) {
	handler := &handlerFijo{outcome: OutcomeAck}
	consumer := NewConsumer(nil, testQueue, handler)

	consumer.process(context.Background(), amqp.Delivery{
		Acknowledger: &fakeAcknowledger{},
		Body:         []byte(`{}`),
		Headers: amqp.Table{
			"x-death": []interface{}{
				amqp.Table{"queue": testQueue, "reason": "rejected", "count": int64(2)},
				amqp.Table{"queue": testQueue + ".retry", "reason": "expired", "count": int64(2)},
			},
		},
	})

	if len(handler.retries) != 1 || handler.retries[0] != 2 {
		t.Fatalf("retries = %v, esperaba [2]", handler.retries)
	}
}

func TestConsumerConMessageHandlerAgotaReintentosYTerminaEnLaDLQ(t *testing.T) {
	proc := &procesadorRegistrado{err: errors.New("base de datos caída")}
	dlq := &fakeDeadLetterPublisher{}
	consumer := NewConsumer(nil, testQueue, nuevoHandler(proc, dlq))
	ack := &fakeAcknowledger{}

	consumer.process(context.Background(), amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  9,
		Body:         envelopeValido(uuid.New().String()),
		Headers: amqp.Table{
			"x-death": []interface{}{
				amqp.Table{"queue": testQueue, "reason": "rejected", "count": int64(testMaxRetries)},
			},
		},
	})

	if len(ack.acked) != 1 || ack.acked[0] != 9 {
		t.Fatalf("esperaba Ack(9) tras publicar en la DLQ, obtuvo acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	if len(dlq.published) != 1 || dlq.published[0].queue != testQueue {
		t.Fatalf("DLQ = %v, esperaba 1 mensaje con routing key %q", dlq.published, testQueue)
	}
}

func TestConsumerRunConsumeConAckManualDeLaColaDeTrabajo(t *testing.T) {
	channel := &fakeConsumerChannel{consumeDeliveries: make(chan amqp.Delivery)}
	consumer := NewConsumer(channel, testQueue, &handlerFijo{outcome: OutcomeAck})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := consumer.Run(ctx); err != nil {
		t.Fatalf("Run devolvió un error inesperado: %v", err)
	}
	if channel.consumeQueue != testQueue {
		t.Errorf("cola consumida = %q, esperaba %q", channel.consumeQueue, testQueue)
	}
	if channel.consumeAutoAck {
		t.Error("autoAck debe ser false: el ack es manual, tras procesar con éxito")
	}
}

func TestConsumerRunPropagaElErrorDeConsume(t *testing.T) {
	channel := &fakeConsumerChannel{consumeErr: errors.New("cola inexistente")}
	consumer := NewConsumer(channel, testQueue, &handlerFijo{})

	if err := consumer.Run(context.Background()); err == nil {
		t.Fatal("esperaba que se propagara el error de Consume")
	}
}

func TestConsumerRunCanalCerradoSinCancelacionDevuelveErrConnectionLost(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	consumer := NewConsumer(&fakeConsumerChannel{consumeDeliveries: deliveries}, testQueue, &handlerFijo{})

	err := consumer.Run(context.Background())

	if !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("error = %v, esperaba ErrConnectionLost", err)
	}
}

func TestConsumerRunCanalCerradoConContextoCanceladoNoEsUnaCaida(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	consumer := NewConsumer(&fakeConsumerChannel{consumeDeliveries: deliveries}, testQueue, &handlerFijo{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := consumer.Run(ctx); err != nil {
		t.Fatalf("error = %v, un cierre con el contexto cancelado responde a un apagado pedido", err)
	}
}

func TestConsumerRunElApagadoEsperaAlMensajeEnVuelo(t *testing.T) {
	deliveries := make(chan amqp.Delivery, 1)
	handler := &handlerFijo{outcome: OutcomeAck, enCurso: make(chan struct{}), liberar: make(chan struct{})}
	consumer := NewConsumer(&fakeConsumerChannel{consumeDeliveries: deliveries}, testQueue, handler)
	ack := &fakeAcknowledger{}
	deliveries <- amqp.Delivery{Acknowledger: ack, DeliveryTag: 5, Body: []byte(`{}`)}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	<-handler.enCurso // el mensaje está en proceso
	cancel()          // SIGTERM a mitad del procesamiento

	select {
	case <-done:
		t.Fatal("Run no debe devolver mientras el mensaje en vuelo sigue procesándose")
	case <-time.After(50 * time.Millisecond):
	}

	close(handler.liberar)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run devolvió un error inesperado: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run no terminó tras completar el mensaje en vuelo")
	}
	if len(ack.acked) != 1 || ack.acked[0] != 5 {
		t.Errorf("el mensaje en vuelo debía confirmarse antes de terminar: acked=%v", ack.acked)
	}
}

// El apagado no debe cancelar el contexto del mensaje en vuelo: con él cancelado
// la consulta a la base fallaría, se lo clasificaría como transitorio y gastaría
// un reintento (o iría a la DLQ con MAX_RETRIES=0) por un simple despliegue.
func TestConsumerRunElMensajeEnVueloNoHeredaLaCancelacionDelApagado(t *testing.T) {
	deliveries := make(chan amqp.Delivery, 1)
	handler := &handlerFijo{outcome: OutcomeAck, enCurso: make(chan struct{}), liberar: make(chan struct{})}
	consumer := NewConsumer(&fakeConsumerChannel{consumeDeliveries: deliveries}, testQueue, handler)
	deliveries <- amqp.Delivery{Acknowledger: &fakeAcknowledger{}, DeliveryTag: 1, Body: []byte(`{}`)}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	<-handler.enCurso
	cancel()
	close(handler.liberar)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run no terminó tras completar el mensaje en vuelo")
	}
	if handler.ctxAlFinal != nil {
		t.Fatalf("el contexto del mensaje en vuelo quedó cancelado por el apagado: %v", handler.ctxAlFinal)
	}
}
