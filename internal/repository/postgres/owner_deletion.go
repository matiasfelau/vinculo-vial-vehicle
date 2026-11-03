package postgres

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DeleteByOwner borra todos los vehículos del titular y retorna cuántos borró
// (US-11, baja de cuenta). Es un único DELETE en una transacción explícita, y
// repetirlo no encuentra filas, así que devuelve 0 sin error. La transacción es
// explícita (no la implícita de GORM) para que un plazo vencido llegue con su
// causa, context.DeadlineExceeded, y classifyError lo reconozca como
// indisponibilidad.
//
// Solo toca las filas del titular. Otro titular que vinculó la misma patente
// tiene su propia fila (UNIQUE(owner_id, pat_hashed)) y la conserva. Tampoco
// toca pending_plates: la reserva de una patente no depende de quién la vinculó.
func (r *VehicleRepository) DeleteByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var removed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("owner_id = ?", ownerID).Delete(&vehicleModel{})
		removed = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return 0, classifyError(err)
	}
	return removed, nil
}
