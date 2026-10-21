package sweeper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu       sync.Mutex
	deleted  int64
	err      error
	calls    int
	lastNow  time.Time
	notifyCh chan struct{}
}

func (f *fakeStore) DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastNow = now
	if f.notifyCh != nil {
		select {
		case f.notifyCh <- struct{}{}:
		default:
		}
	}
	return f.deleted, f.err
}

func (f *fakeStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunOnceBorraLasVencidasAlInstanteActual(t *testing.T) {
	store := &fakeStore{deleted: 3}
	sweeper := NewPendingSweeper(store)
	ahora := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	sweeper.now = func() time.Time { return ahora }

	borradas, err := sweeper.RunOnce(context.Background())

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if borradas != 3 {
		t.Fatalf("esperaba 3 filas borradas, obtuvo %d", borradas)
	}
	if !store.lastNow.Equal(ahora) {
		t.Fatalf("now = %v, esperaba %v", store.lastNow, ahora)
	}
}

func TestRunOnceEsIdempotenteSinVencidas(t *testing.T) {
	store := &fakeStore{}
	sweeper := NewPendingSweeper(store)

	for i := 0; i < 2; i++ {
		borradas, err := sweeper.RunOnce(context.Background())
		if err != nil || borradas != 0 {
			t.Fatalf("vuelta %d: esperaba (0, nil), obtuvo (%d, %v)", i, borradas, err)
		}
	}
}

func TestRunOnceError(t *testing.T) {
	boom := errors.New("base caída")
	sweeper := NewPendingSweeper(&fakeStore{err: boom})

	if _, err := sweeper.RunOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("esperaba propagar el error del almacén, obtuvo %v", err)
	}
}

func TestRunSigueTrasUnErrorYTerminaAlCancelarElContexto(t *testing.T) {
	store := &fakeStore{err: errors.New("base caída"), notifyCh: make(chan struct{}, 8)}
	sweeper := NewPendingSweeper(store)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		sweeper.Run(ctx, 5*time.Millisecond)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-store.notifyCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("el sweeper no volvió a correr tras un error (vueltas: %d)", store.callCount())
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run no terminó al cancelarse el contexto")
	}
}
