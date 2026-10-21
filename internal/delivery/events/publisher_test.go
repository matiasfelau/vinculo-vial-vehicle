package events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

type published struct {
	exchange  string
	key       string
	mandatory bool
	immediate bool
	msg       amqp.Publishing
}

type fakeChannel struct {
	acked    bool
	err      error
	block    bool
	received []published
}

func (f *fakeChannel) PublishAndConfirm(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (bool, error) {
	f.received = append(f.received, published{exchange, key, mandatory, immediate, msg})
	if f.block {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return f.acked, f.err
}

func sampleEvent(actor *uuid.UUID) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:        uuid.New(),
		EventType: "vehicle.pending.claimed",
		Actor:     actor,
		Payload:   json.RawMessage(`{"pending_ref":"3f2c1a9e-6c1d-4a58-9d0a-0d6f1c1b2e11"}`),
		CreatedAt: time.Now().UTC(),
	}
}

func TestPublishArmaElEnvelopeEstandar(t *testing.T) {
	actor := uuid.New()
	event := sampleEvent(&actor)
	channel := &fakeChannel{acked: true}
	publisher := NewPublisher(channel, "vvi.topic.exchange", time.Second)

	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if len(channel.received) != 1 {
		t.Fatalf("esperaba 1 publicación, obtuvo %d", len(channel.received))
	}
	got := channel.received[0]
	if got.exchange != "vvi.topic.exchange" || got.key != "vehicle.pending.claimed" {
		t.Fatalf("exchange/routing key = %q/%q", got.exchange, got.key)
	}
	var envelope EventEnvelope
	if err := json.Unmarshal(got.msg.Body, &envelope); err != nil {
		t.Fatalf("el cuerpo no es un envelope: %v", err)
	}
	if envelope.EventID != event.ID.String() {
		t.Fatalf("eventId = %q, esperaba el ID del outbox %q", envelope.EventID, event.ID)
	}
	if envelope.EventType != "vehicle.pending.claimed" {
		t.Fatalf("eventType = %q", envelope.EventType)
	}
	if envelope.Producer != "vinculo-vial-vehicle" {
		t.Fatalf("producer = %q", envelope.Producer)
	}
	if envelope.Actor == nil || *envelope.Actor != actor.String() {
		t.Fatalf("actor = %v, esperaba %v", envelope.Actor, actor)
	}
	if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
		t.Fatalf("timestamp no es RFC 3339: %q", envelope.Timestamp)
	}
	if string(envelope.Payload) != string(event.Payload) {
		t.Fatalf("payload = %s, esperaba %s", envelope.Payload, event.Payload)
	}
}

func TestPublishSinActorSerializaNull(t *testing.T) {
	channel := &fakeChannel{acked: true}
	publisher := NewPublisher(channel, "vvi.topic.exchange", time.Second)

	if err := publisher.Publish(context.Background(), sampleEvent(nil)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if !strings.Contains(string(channel.received[0].msg.Body), `"actor":null`) {
		t.Fatalf("esperaba actor null en el envelope, obtuvo %s", channel.received[0].msg.Body)
	}
}

func TestPublishEsPersistenteYMandatory(t *testing.T) {
	channel := &fakeChannel{acked: true}
	publisher := NewPublisher(channel, "vvi.topic.exchange", time.Second)

	if err := publisher.Publish(context.Background(), sampleEvent(nil)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	got := channel.received[0]
	if got.msg.DeliveryMode != amqp.Persistent {
		t.Fatalf("DeliveryMode = %d, esperaba persistente", got.msg.DeliveryMode)
	}
	if !got.mandatory || got.immediate {
		t.Fatalf("mandatory=%v immediate=%v, esperaba mandatory=true immediate=false", got.mandatory, got.immediate)
	}
	if got.msg.ContentType != "application/json" {
		t.Fatalf("ContentType = %q", got.msg.ContentType)
	}
}

func TestPublishNackDelBrokerEsError(t *testing.T) {
	publisher := NewPublisher(&fakeChannel{acked: false}, "vvi.topic.exchange", time.Second)

	if err := publisher.Publish(context.Background(), sampleEvent(nil)); err == nil {
		t.Fatal("un nack del broker debía ser un error: el evento no puede marcarse enviado")
	}
}

func TestPublishErrorDelCanalSePropaga(t *testing.T) {
	boom := errors.New("canal caído")
	publisher := NewPublisher(&fakeChannel{err: boom}, "vvi.topic.exchange", time.Second)

	if err := publisher.Publish(context.Background(), sampleEvent(nil)); !errors.Is(err, boom) {
		t.Fatalf("esperaba propagar el error del canal, obtuvo %v", err)
	}
}

func TestPublishTimeoutAcotaLaEsperaDelConfirm(t *testing.T) {
	publisher := NewPublisher(&fakeChannel{block: true}, "vvi.topic.exchange", 30*time.Millisecond)

	inicio := time.Now()
	err := publisher.Publish(context.Background(), sampleEvent(nil))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("esperaba context.DeadlineExceeded, obtuvo %v", err)
	}
	if time.Since(inicio) > time.Second {
		t.Fatal("el timeout de publicación no cortó la espera")
	}
}

func TestPublishConIDVacioGeneraUnEventIDNuevo(t *testing.T) {
	channel := &fakeChannel{acked: true}
	publisher := NewPublisher(channel, "vvi.topic.exchange", time.Second)
	event := sampleEvent(nil)
	event.ID = uuid.Nil

	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	var envelope EventEnvelope
	if err := json.Unmarshal(channel.received[0].msg.Body, &envelope); err != nil {
		t.Fatalf("el cuerpo no es un envelope: %v", err)
	}
	if _, err := uuid.Parse(envelope.EventID); err != nil || envelope.EventID == uuid.Nil.String() {
		t.Fatalf("eventId inválido: %q", envelope.EventID)
	}
}
