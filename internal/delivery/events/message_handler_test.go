package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testMaxRetries = 3

// procesadorRegistrado cuenta las invocaciones y devuelve un error
// configurable, o entra en pánico.
type procesadorRegistrado struct {
	err       error
	panicWith any
	calls     int
	last      InboundEnvelope
}

func (p *procesadorRegistrado) Process(_ context.Context, envelope InboundEnvelope) error {
	p.calls++
	p.last = envelope
	if p.panicWith != nil {
		panic(p.panicWith)
	}
	return p.err
}

func nuevoHandler(p Processor, dlq deadLetterPublisher) *MessageHandler {
	return NewMessageHandler(p, dlq, testQueue, testMaxRetries)
}

func TestMessageHandlerEventoValidoHaceAckYEntregaElEnvelopeAlProcesador(t *testing.T) {
	proc := &procesadorRegistrado{}
	dlq := &fakeDeadLetterPublisher{}
	eventID := uuid.New()

	outcome := nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(eventID.String()), 0)

	if outcome != OutcomeAck {
		t.Fatalf("outcome = %v, esperaba OutcomeAck", outcome)
	}
	if proc.calls != 1 || proc.last.EventID != eventID {
		t.Errorf("el procesador debía recibir el envelope una vez (calls=%d, eventId=%v)", proc.calls, proc.last.EventID)
	}
	if len(dlq.published) != 0 {
		t.Error("un evento exitoso no debe ir a la DLQ")
	}
}

func TestMessageHandlerEnvelopeInvalidoVaDirectoALaDLQSinReintentoYSinProcesar(t *testing.T) {
	proc := &procesadorRegistrado{}
	dlq := &fakeDeadLetterPublisher{}
	body := []byte(`{no es json`)

	outcome := nuevoHandler(proc, dlq).Handle(context.Background(), body, 0)

	if outcome != OutcomeAck {
		t.Fatalf("outcome = %v, esperaba OutcomeAck tras la confirmación de la DLQ", outcome)
	}
	if proc.calls != 0 {
		t.Error("un envelope inválido no debe llegar al procesador")
	}
	if len(dlq.published) != 1 {
		t.Fatalf("esperaba 1 mensaje en la DLQ, hubo %d", len(dlq.published))
	}
	if dlq.published[0].queue != testQueue {
		t.Errorf("la DLQ se publica por el nombre de la cola de trabajo: queue = %q, esperaba %q", dlq.published[0].queue, testQueue)
	}
	if string(dlq.published[0].body) != string(body) {
		t.Error("la DLQ debe recibir el cuerpo original sin modificar")
	}
}

func TestMessageHandlerErrorPermanenteDelProcesadorVaDirectoALaDLQAunSinReintentosPrevios(t *testing.T) {
	proc := &procesadorRegistrado{err: fmt.Errorf("payload sin deleted_at: %w", ErrInvalidMessage)}
	dlq := &fakeDeadLetterPublisher{}

	outcome := nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(uuid.New().String()), 0)

	if outcome != OutcomeAck {
		t.Fatalf("outcome = %v, esperaba OutcomeAck", outcome)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("un mensaje inválido va directo a la DLQ sin pasar por retry: DLQ=%d", len(dlq.published))
	}
}

func TestMessageHandlerErrorTransitorioRechazaSinRequeueMientrasQuedenReintentos(t *testing.T) {
	for retries := int64(0); retries < testMaxRetries; retries++ {
		proc := &procesadorRegistrado{err: errors.New("base de datos caída")}
		dlq := &fakeDeadLetterPublisher{}

		outcome := nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(uuid.New().String()), retries)

		if outcome != OutcomeNackNoRequeue {
			t.Fatalf("retries=%d: outcome = %v, esperaba OutcomeNackNoRequeue", retries, outcome)
		}
		if len(dlq.published) != 0 {
			t.Errorf("retries=%d: no debía publicarse en la DLQ antes de agotar los reintentos", retries)
		}
	}
}

func TestMessageHandlerAgotadoMaxRetriesPublicaEnLaDLQYHaceAck(t *testing.T) {
	proc := &procesadorRegistrado{err: errors.New("base de datos caída")}
	dlq := &fakeDeadLetterPublisher{}

	outcome := nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(uuid.New().String()), testMaxRetries)

	if outcome != OutcomeAck {
		t.Fatalf("outcome = %v, esperaba OutcomeAck tras la confirmación de la DLQ", outcome)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("al agotar MAX_RETRIES el mensaje va a la DLQ: DLQ=%d", len(dlq.published))
	}
}

func TestMessageHandlerSiLaDLQNoConfirmaNoHaceAckDelOriginal(t *testing.T) {
	casos := map[string]struct {
		body    []byte
		err     error
		retries int64
	}{
		"envelope inválido":   {body: []byte(`{`)},
		"error permanente":    {body: envelopeValido(uuid.New().String()), err: ErrInvalidMessage},
		"reintentos agotados": {body: envelopeValido(uuid.New().String()), err: errors.New("caída"), retries: testMaxRetries},
	}

	for nombre, caso := range casos {
		t.Run(nombre, func(t *testing.T) {
			proc := &procesadorRegistrado{err: caso.err}
			dlq := &fakeDeadLetterPublisher{err: errors.New("dlq sin confirmar")}

			outcome := nuevoHandler(proc, dlq).Handle(context.Background(), caso.body, caso.retries)

			if outcome != OutcomeNackNoRequeue {
				t.Fatalf("outcome = %v: sin confirm de la DLQ el original no puede confirmarse, esperaba OutcomeNackNoRequeue", outcome)
			}
		})
	}
}

func TestMessageHandlerPanicDelProcesadorSeCapturaYVaALaDLQ(t *testing.T) {
	proc := &procesadorRegistrado{panicWith: "índice fuera de rango"}
	dlq := &fakeDeadLetterPublisher{}

	var outcome Outcome
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("el panic del procesador se propagó y mataría al consumidor: %v", r)
			}
		}()
		outcome = nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(uuid.New().String()), 0)
	}()

	if outcome != OutcomeAck {
		t.Fatalf("outcome = %v, esperaba OutcomeAck tras derivar a la DLQ", outcome)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("un panic es permanente y va directo a la DLQ: DLQ=%d", len(dlq.published))
	}
}

func TestMessageHandlerRazonDeDescarteNoReflejaElContenidoDelMensaje(t *testing.T) {
	dlq := &fakeDeadLetterPublisher{}
	proc := &procesadorRegistrado{panicWith: "SECRETO-CRIPTOGRAMA"}

	nuevoHandler(proc, dlq).Handle(context.Background(), envelopeValido(uuid.New().String()), 0)

	if len(dlq.published) != 1 {
		t.Fatalf("DLQ=%d", len(dlq.published))
	}
	if strings.Contains(dlq.published[0].reason, "SECRETO-CRIPTOGRAMA") {
		t.Errorf("x-discard-reason no debe incluir el valor del panic: %q", dlq.published[0].reason)
	}
}
