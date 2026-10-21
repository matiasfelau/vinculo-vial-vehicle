package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// insertOutboxEvent persiste el evento como pendiente usando tx, de modo que
// quede en la misma transacción que el cambio de estado que lo origina. Un
// evento sin fila persistida es indistinguible de uno que nunca ocurrió.
func insertOutboxEvent(tx *gorm.DB, event domain.OutboxEvent) error {
	return tx.Create(&outboxEventModel{
		ID:        event.ID,
		EventType: event.EventType,
		Actor:     event.Actor,
		Payload:   string(event.Payload),
		Status:    outboxStatusPending,
		CreatedAt: event.CreatedAt,
	}).Error
}

// OutboxRepository implementa domain.OutboxRepository sobre GORM/PostgreSQL.
type OutboxRepository struct {
	db           *gorm.DB
	queryTimeout time.Duration
}

var _ domain.OutboxRepository = (*OutboxRepository)(nil)

// NewOutboxRepository construye un OutboxRepository sobre la conexión dada.
// queryTimeout acota cada consulta, igual que WithQueryTimeout en
// VehicleRepository (DB_QUERY_TIMEOUT_MS).
func NewOutboxRepository(db *gorm.DB, queryTimeout time.Duration) *OutboxRepository {
	return &OutboxRepository{db: db, queryTimeout: queryTimeout}
}

// FetchPending devuelve hasta limit eventos todavía no enviados, de los más
// viejos a los más nuevos, para no dejar eventos varados detrás de un alud de
// eventos nuevos.
func (r *OutboxRepository) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var rows []outboxEventModel
	err := r.db.WithContext(ctx).
		Where("status = ?", outboxStatusPending).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, classifyError(err)
	}

	events := make([]domain.OutboxEvent, len(rows))
	for i, row := range rows {
		events[i] = domain.OutboxEvent{
			ID:        row.ID,
			EventType: row.EventType,
			Actor:     row.Actor,
			Payload:   json.RawMessage(row.Payload),
			CreatedAt: row.CreatedAt,
		}
	}
	return events, nil
}

// MarkSent registra que el evento se publicó y el broker lo confirmó.
func (r *OutboxRepository) MarkSent(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	err := r.db.WithContext(ctx).Model(&outboxEventModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":  outboxStatusSent,
			"sent_at": time.Now().UTC(),
		}).Error
	return classifyError(err)
}

// MarkFailed registra un intento fallido sin cambiar el estado pendiente: el
// worker lo reintenta en el próximo ciclo.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	err := r.db.WithContext(ctx).Model(&outboxEventModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": lastErr,
		}).Error
	return classifyError(err)
}
