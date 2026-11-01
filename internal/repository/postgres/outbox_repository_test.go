package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func outboxEvent(createdAt time.Time, actor *uuid.UUID) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:        uuid.New(),
		EventType: "vehicle.pending.claimed",
		Actor:     actor,
		Payload:   json.RawMessage(`{"pending_ref":"3f2c1a9e-6c1d-4a58-9d0a-0d6f1c1b2e11"}`),
		CreatedAt: createdAt,
	}
}

func enqueue(t *testing.T, db *gorm.DB, events ...domain.OutboxEvent) {
	t.Helper()

	for _, event := range events {
		if err := insertOutboxEvent(db, event); err != nil {
			t.Fatalf("no se pudo encolar el evento: %v", err)
		}
	}
}

func TestOutboxFetchPendingDevuelveLosMasViejosPrimeroHastaElLimite(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOutboxRepository(db, time.Second)
	ahora := time.Now().UTC()
	nuevo := outboxEvent(ahora, nil)
	viejo := outboxEvent(ahora.Add(-2*time.Hour), nil)
	medio := outboxEvent(ahora.Add(-time.Hour), nil)
	enqueue(t, db, nuevo, viejo, medio)

	events, err := repo.FetchPending(context.Background(), 2)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(events) != 2 || events[0].ID != viejo.ID || events[1].ID != medio.ID {
		t.Fatalf("esperaba [viejo, medio], obtuvo %v", events)
	}
}

func TestOutboxFetchPendingConservaActorPayloadYTipo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOutboxRepository(db, time.Second)
	actor := uuid.New()
	event := outboxEvent(time.Now().UTC(), &actor)
	enqueue(t, db, event)

	events, err := repo.FetchPending(context.Background(), 10)

	if err != nil || len(events) != 1 {
		t.Fatalf("esperaba 1 evento sin error, obtuvo %d (%v)", len(events), err)
	}
	got := events[0]
	if got.ID != event.ID || got.EventType != event.EventType {
		t.Fatalf("ID/tipo = %v/%q", got.ID, got.EventType)
	}
	if got.Actor == nil || *got.Actor != actor {
		t.Fatalf("actor = %v, esperaba %v", got.Actor, actor)
	}
	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil || payload["pending_ref"] != "3f2c1a9e-6c1d-4a58-9d0a-0d6f1c1b2e11" {
		t.Fatalf("payload = %s (%v)", got.Payload, err)
	}
}

func TestOutboxMarkSentLoSacaDeLosPendientes(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOutboxRepository(db, time.Second)
	event := outboxEvent(time.Now().UTC(), nil)
	enqueue(t, db, event)

	if err := repo.MarkSent(context.Background(), event.ID); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	pendientes, err := repo.FetchPending(context.Background(), 10)
	if err != nil || len(pendientes) != 0 {
		t.Fatalf("esperaba 0 pendientes, obtuvo %d (%v)", len(pendientes), err)
	}
	var row outboxEventModel
	if err := db.First(&row, "id = ?", event.ID).Error; err != nil {
		t.Fatalf("no se pudo leer la fila: %v", err)
	}
	if row.Status != outboxStatusSent || row.SentAt == nil {
		t.Fatalf("status=%q sent_at=%v, esperaba sent con fecha", row.Status, row.SentAt)
	}
}

func TestOutboxMarkFailedCuentaIntentosYMantieneElEventoPendiente(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOutboxRepository(db, time.Second)
	event := outboxEvent(time.Now().UTC(), nil)
	enqueue(t, db, event)

	for i := 0; i < 2; i++ {
		if err := repo.MarkFailed(context.Background(), event.ID, "broker caído"); err != nil {
			t.Fatalf("no se esperaba error: %v", err)
		}
	}

	var row outboxEventModel
	if err := db.First(&row, "id = ?", event.ID).Error; err != nil {
		t.Fatalf("no se pudo leer la fila: %v", err)
	}
	if row.Attempts != 2 || row.LastError == nil || *row.LastError != "broker caído" {
		t.Fatalf("attempts=%d last_error=%v", row.Attempts, row.LastError)
	}
	if row.Status != outboxStatusPending {
		t.Fatalf("status = %q, el evento debía seguir pendiente", row.Status)
	}
}

// El evento tiene que quedar o no según el commit de la transacción que lo encola:
// ese es el contrato del outbox.
func TestInsertOutboxEventRespetaElRollbackDeLaTransaccion(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOutboxRepository(db, time.Second)
	event := outboxEvent(time.Now().UTC(), nil)

	_ = db.Transaction(func(tx *gorm.DB) error {
		if err := insertOutboxEvent(tx, event); err != nil {
			t.Fatalf("no se pudo encolar: %v", err)
		}
		return context.Canceled // fuerza el rollback
	})

	pendientes, err := repo.FetchPending(context.Background(), 10)
	if err != nil || len(pendientes) != 0 {
		t.Fatalf("tras el rollback no debía quedar ningún evento, obtuvo %d (%v)", len(pendientes), err)
	}
}
