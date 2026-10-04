package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// fakeServer simula un *http.Server sin abrir sockets reales. listenBlock se
// cierra cuando Shutdown termina de drenar, simulando que ListenAndServe
// retorna una vez que el servidor deja de aceptar conexiones. enVuelo
// modela una petición en curso: Shutdown no retorna hasta que se cierra,
// permitiendo verificar que el drenaje espera a que termine.
type fakeServer struct {
	mu              sync.Mutex
	shutdownLlamado bool
	shutdownCtx     context.Context

	listenErr   error // si no es nil, ListenAndServe retorna este error de inmediato
	listenBlock chan struct{}
	enVuelo     chan struct{}
}

func nuevoFakeServer() *fakeServer {
	return &fakeServer{
		listenBlock: make(chan struct{}),
		enVuelo:     make(chan struct{}),
	}
}

func (f *fakeServer) ListenAndServe() error {
	if f.listenErr != nil {
		return f.listenErr
	}
	<-f.listenBlock
	return http.ErrServerClosed
}

func (f *fakeServer) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	f.shutdownLlamado = true
	f.shutdownCtx = ctx
	f.mu.Unlock()

	<-f.enVuelo
	close(f.listenBlock)
	return nil
}

func (f *fakeServer) fueShutdownLlamado() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdownLlamado
}

func TestRunUntilSignalDrenaLaPeticionEnVueloAntesDeRetornar(t *testing.T) {
	srv := nuevoFakeServer()
	ctx, cancel := context.WithCancel(context.Background())

	resultado := make(chan error, 1)
	go func() {
		resultado <- RunUntilSignal(ctx, []Server{srv}, 5*time.Second)
	}()

	cancel() // simula SIGTERM

	// Espera a que Shutdown haya sido invocado, sin depender de un sleep fijo.
	deadline := time.After(time.Second)
	for !srv.fueShutdownLlamado() {
		select {
		case <-deadline:
			t.Fatal("Shutdown nunca fue invocado tras cancelar el contexto")
		case <-time.After(time.Millisecond):
		}
	}

	// La petición en vuelo aún no terminó: RunUntilSignal no debe haber retornado.
	select {
	case err := <-resultado:
		t.Fatalf("esperaba que RunUntilSignal siguiera esperando la petición en vuelo, retornó %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(srv.enVuelo) // la petición en vuelo completa

	select {
	case err := <-resultado:
		if err != nil {
			t.Fatalf("no se esperaba error, obtuvo %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunUntilSignal no retornó tras completarse la petición en vuelo")
	}
}

func TestRunUntilSignalPropagaErrorSiUnServidorFallaAntesDeLaSenal(t *testing.T) {
	errEsperado := errors.New("puerto ya en uso (prueba)")
	srv := nuevoFakeServer()
	srv.listenErr = errEsperado

	ctx := context.Background() // nunca se cancela en este test

	err := RunUntilSignal(ctx, []Server{srv}, time.Second)
	if !errors.Is(err, errEsperado) {
		t.Fatalf("esperaba %v, obtuvo %v", errEsperado, err)
	}
}

func TestRunUntilSignalInvocaShutdownEnTodosLosServidoresConElMismoContexto(t *testing.T) {
	srv1 := nuevoFakeServer()
	srv2 := nuevoFakeServer()
	close(srv1.enVuelo)
	close(srv2.enVuelo)

	ctx, cancel := context.WithCancel(context.Background())

	resultado := make(chan error, 1)
	go func() {
		resultado <- RunUntilSignal(ctx, []Server{srv1, srv2}, time.Second)
	}()

	cancel()

	select {
	case err := <-resultado:
		if err != nil {
			t.Fatalf("no se esperaba error, obtuvo %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunUntilSignal no retornó")
	}

	if !srv1.fueShutdownLlamado() || !srv2.fueShutdownLlamado() {
		t.Fatal("esperaba que Shutdown se invocara en ambos servidores")
	}
	if srv1.shutdownCtx != srv2.shutdownCtx {
		t.Fatal("esperaba que ambos servidores recibieran el mismo contexto de apagado")
	}
}
