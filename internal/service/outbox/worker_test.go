package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

type fakeRepo struct {
	mu          sync.Mutex
	pending     []domain.OutboxEvent
	fetchErr    error
	markSentErr error
	sent        []uuid.UUID
	failed      map[uuid.UUID]string
	limit       int
}

func (f *fakeRepo) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limit = limit
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return append([]domain.OutboxEvent(nil), f.pending...), nil
}

func (f *fakeRepo) MarkSent(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markSentErr != nil {
		return f.markSentErr
	}
	f.sent = append(f.sent, id)
	return nil
}

func (f *fakeRepo) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed == nil {
		f.failed = make(map[uuid.UUID]string)
	}
	f.failed[id] = lastErr
	return nil
}

type fakePublisher struct {
	failFor   map[uuid.UUID]error
	published []uuid.UUID
}

func (f *fakePublisher) Publish(ctx context.Context, event domain.OutboxEvent) error {
	if err := f.failFor[event.ID]; err != nil {
		return err
	}
	f.published = append(f.published, event.ID)
	return nil
}

func newEvents(n int) []domain.OutboxEvent {
	events := make([]domain.OutboxEvent, n)
	for i := range events {
		events[i] = domain.OutboxEvent{ID: uuid.New(), EventType: "vehicle.pending.claimed", CreatedAt: time.Now()}
	}
	return events
}

func TestProcessBatchPublicaYMarcaEnviado(t *testing.T) {
	events := newEvents(3)
	repo := &fakeRepo{pending: events}
	publisher := &fakePublisher{}
	worker := NewWorker(repo, publisher, 20)

	sent, err := worker.ProcessBatch(context.Background())

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if sent != 3 || len(repo.sent) != 3 || len(publisher.published) != 3 {
		t.Fatalf("esperaba 3 publicados y marcados, obtuvo sent=%d marcados=%d publicados=%d", sent, len(repo.sent), len(publisher.published))
	}
	if repo.limit != 20 {
		t.Fatalf("el lote pedido fue %d, esperaba 20", repo.limit)
	}
}

func TestProcessBatchFalloDePublishNoMarcaEnviadoYRegistraElIntento(t *testing.T) {
	events := newEvents(3)
	boom := errors.New("broker caído")
	repo := &fakeRepo{pending: events}
	publisher := &fakePublisher{failFor: map[uuid.UUID]error{events[1].ID: boom}}
	worker := NewWorker(repo, publisher, 20)

	sent, err := worker.ProcessBatch(context.Background())

	if err != nil {
		t.Fatalf("un fallo de publicación no es un error del lote: %v", err)
	}
	if sent != 2 {
		t.Fatalf("esperaba 2 publicados, obtuvo %d", sent)
	}
	for _, id := range repo.sent {
		if id == events[1].ID {
			t.Fatal("un evento cuyo publish falló no debía marcarse enviado")
		}
	}
	if repo.failed[events[1].ID] != boom.Error() {
		t.Fatalf("last_error = %q, esperaba %q", repo.failed[events[1].ID], boom.Error())
	}
}

func TestProcessBatchPublicadoPeroSinMarcarQuedaPendienteParaElProximoCiclo(t *testing.T) {
	events := newEvents(1)
	repo := &fakeRepo{pending: events, markSentErr: errors.New("base caída")}
	worker := NewWorker(repo, &fakePublisher{}, 20)

	sent, err := worker.ProcessBatch(context.Background())

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if sent != 0 {
		t.Fatalf("un evento sin marcar no cuenta como enviado, obtuvo %d", sent)
	}
	if !worker.Healthy() {
		t.Fatal("poder leer el outbox mantiene sano al worker")
	}
}

func TestProcessBatchReintentaConElMismoIDAlRepublicar(t *testing.T) {
	events := newEvents(1)
	repo := &fakeRepo{pending: events, markSentErr: errors.New("base caída")}
	publisher := &fakePublisher{}
	worker := NewWorker(repo, publisher, 20)

	for i := 0; i < 2; i++ {
		if _, err := worker.ProcessBatch(context.Background()); err != nil {
			t.Fatalf("ciclo %d: %v", i, err)
		}
	}

	if len(publisher.published) != 2 || publisher.published[0] != publisher.published[1] {
		t.Fatalf("el reintento debía republicar el mismo evento (mismo eventId): %v", publisher.published)
	}
}

func TestProcessBatchErrorDeLecturaMarcaNoSaludable(t *testing.T) {
	repo := &fakeRepo{fetchErr: errors.New("base caída")}
	worker := NewWorker(repo, &fakePublisher{}, 20)

	if _, err := worker.ProcessBatch(context.Background()); err == nil {
		t.Fatal("esperaba error al no poder leer el outbox")
	}
	if worker.Healthy() {
		t.Fatal("sin poder leer el outbox el worker no debía reportarse sano")
	}

	repo.mu.Lock()
	repo.fetchErr = nil
	repo.mu.Unlock()
	if _, err := worker.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !worker.Healthy() {
		t.Fatal("tras un ciclo exitoso el worker debía volver a ser sano")
	}
}

func TestRunProcesaPeriodicamenteYTerminaAlCancelar(t *testing.T) {
	repo := &fakeRepo{pending: newEvents(1)}
	worker := NewWorker(repo, &fakePublisher{}, 20)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, 5*time.Millisecond)
	}()

	deadline := time.After(2 * time.Second)
	for {
		repo.mu.Lock()
		n := len(repo.sent)
		repo.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("el worker no procesó el evento pendiente")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run no terminó al cancelarse el contexto")
	}
}
