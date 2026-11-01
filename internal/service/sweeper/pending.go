// Package sweeper ejecuta las limpiezas periódicas del servicio: hoy, el borrado
// de las patentes pendientes de entrega (US-10) que vencieron.
package sweeper

import (
	"context"
	"log/slog"
	"time"
)

// PendingStore es lo que el sweeper necesita de la persistencia de patentes
// pendientes.
type PendingStore interface {
	DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error)
}

// PendingSweeper borra periódicamente las patentes pendientes vencidas
// (expires_at <= now). La vigencia ya se verifica al leer y al reclamar, así que
// el sweeper solo acota el tamaño de la tabla y, con ello, cuánto tiempo viven
// las referencias de una patente sin dueños.
type PendingSweeper struct {
	store PendingStore
	now   func() time.Time
}

// NewPendingSweeper construye el sweeper sobre el almacén dado.
func NewPendingSweeper(store PendingStore) *PendingSweeper {
	return &PendingSweeper{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// RunOnce ejecuta una vuelta y retorna cuántas filas borró. Es idempotente:
// repetirla, o correrla desde otra réplica, no tiene efectos adicionales.
func (s *PendingSweeper) RunOnce(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredPending(ctx, s.now())
}

// Run ejecuta RunOnce cada interval hasta que ctx se cancele. Un error de una
// vuelta se registra y no corta el ciclo: la próxima vuelta lo reintenta.
func (s *PendingSweeper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := s.RunOnce(ctx)
			if err != nil {
				slog.Error("sweeper: no se pudieron borrar las patentes pendientes vencidas", "error", err.Error())
				continue
			}
			if deleted > 0 {
				slog.Info("sweeper: patentes pendientes vencidas borradas", "cantidad", deleted)
			}
		}
	}
}
