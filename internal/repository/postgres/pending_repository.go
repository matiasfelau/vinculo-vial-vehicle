package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// lockPlateSQL toma un advisory lock transaccional sobre la patente. Serializa
// a quienes deciden sobre sus dueños y su reserva pendiente: resolve-owner y la
// vinculación. Sin él, una vinculación concurrente que no ve la reserva todavía
// no confirmada dejaría una pendiente huérfana para una patente que ya tiene
// dueño. Una colisión del hash solo serializa de más, nunca es incorrecta.
const lockPlateSQL = "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))"

// upsertPendingSQL crea la fila pendiente de la patente o, si ya existe, extiende
// su vencimiento. Una fila vencida que el sweeper todavía no borró no se
// reutiliza: sus alertas ya no son entregables, así que se emite un pending_ref
// nuevo. Todas las expresiones del SET leen la fila anterior.
const upsertPendingSQL = `
INSERT INTO pending_plates (plate_hmac, pending_ref, expires_at, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (plate_hmac) DO UPDATE SET
	pending_ref = CASE WHEN pending_plates.expires_at > ? THEN pending_plates.pending_ref ELSE EXCLUDED.pending_ref END,
	created_at  = CASE WHEN pending_plates.expires_at > ? THEN pending_plates.created_at  ELSE EXCLUDED.created_at  END,
	expires_at  = EXCLUDED.expires_at
RETURNING pending_ref`

// lockPlate toma el advisory lock de la patente hasta el fin de la transacción.
func lockPlate(tx *gorm.DB, patHashed string) error {
	return tx.Exec(lockPlateSQL, patHashed).Error
}

// findOwners retorna los owner_id vinculados a ese Blind Index.
func findOwners(tx *gorm.DB, patHashed string) ([]uuid.UUID, error) {
	var ownerIDs []uuid.UUID
	err := tx.Model(&vehicleModel{}).
		Where("pat_hashed = ?", patHashed).
		Pluck("owner_id", &ownerIDs).Error
	return ownerIDs, err
}

// FindOwnersOrReservePending implementa domain.VehicleRepository. Todo ocurre en
// una única transacción bajo el lock de la patente.
func (r *VehicleRepository) FindOwnersOrReservePending(ctx context.Context, patHashed string, proposal domain.PendingProposal) ([]uuid.UUID, *uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var (
		ownerIDs   []uuid.UUID
		pendingRef *uuid.UUID
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPlate(tx, patHashed); err != nil {
			return err
		}

		found, err := findOwners(tx, patHashed)
		if err != nil {
			return err
		}
		if len(found) > 0 {
			ownerIDs = found
			return nil
		}

		var ref uuid.UUID
		row := tx.Raw(upsertPendingSQL, patHashed, proposal.Ref, proposal.ExpiresAt, proposal.Now, proposal.Now, proposal.Now).Row()
		if err := row.Scan(&ref); err != nil {
			return fmt.Errorf("no se pudo reservar la patente pendiente: %w", err)
		}
		pendingRef = &ref
		return nil
	})
	if err != nil {
		return nil, nil, classifyError(err)
	}

	return ownerIDs, pendingRef, nil
}

// DeleteExpiredPending implementa domain.VehicleRepository.
func (r *VehicleRepository) DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	result := r.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&pendingPlateModel{})
	if result.Error != nil {
		return 0, classifyError(result.Error)
	}
	return result.RowsAffected, nil
}
