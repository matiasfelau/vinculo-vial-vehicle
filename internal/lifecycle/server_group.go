// Package lifecycle orquesta el arranque y el apagado ordenado de los
// servidores HTTP del proceso (el público y el interno de US-05), de modo que
// una señal de terminación drene las peticiones en vuelo en lugar de
// cortarlas de golpe.
package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Server es el subconjunto de *http.Server que RunUntilSignal necesita. Se
// declara como interfaz para permitir probar la orquestación con dobles, sin
// abrir sockets reales.
type Server interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// RunUntilSignal arranca cada servidor en su propia goroutine y bloquea hasta
// que ctx se cancele (típicamente por SIGINT/SIGTERM, ver signal.NotifyContext
// en cmd/api) o hasta que alguno de los servidores falle antes de esa señal.
//
// Al cancelarse ctx, se invoca Shutdown en todos los servidores con un plazo
// acotado por shutdownTimeout: las conexiones en vuelo se atienden hasta
// completarse (o hasta agotar el plazo) y no se aceptan conexiones nuevas.
// Retorna el primer error de Shutdown, o nil si todos drenaron correctamente.
func RunUntilSignal(ctx context.Context, servers []Server, shutdownTimeout time.Duration) error {
	errCh := make(chan error, len(servers))
	for _, s := range servers {
		srv := s
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}

	select {
	case err := <-errCh:
		// Un servidor falló (p. ej. el puerto ya estaba en uso) antes de
		// cualquier señal de apagado: se propaga sin esperar a ctx.
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var primerError error
	for _, s := range servers {
		if err := s.Shutdown(shutdownCtx); err != nil && primerError == nil {
			primerError = err
		}
	}
	return primerError
}
