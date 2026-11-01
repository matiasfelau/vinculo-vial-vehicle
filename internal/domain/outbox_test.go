package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewPendingClaimedEventLlevaSoloElPendingRefYElDuenoEnElActor(t *testing.T) {
	id, ownerID, pendingRef := uuid.New(), uuid.New(), uuid.New()
	createdAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	event, err := NewPendingClaimedEvent(id, ownerID, pendingRef, createdAt)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if event.ID != id || event.EventType != "vehicle.pending.claimed" || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("evento inesperado: %+v", event)
	}
	if event.Actor == nil || *event.Actor != ownerID {
		t.Fatalf("actor = %v, esperaba %v", event.Actor, ownerID)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload inválido: %v", err)
	}
	if len(payload) != 1 || payload["pending_ref"] != pendingRef.String() {
		t.Fatalf("el payload debía ser solo {pending_ref}, obtuvo %s", event.Payload)
	}
}
