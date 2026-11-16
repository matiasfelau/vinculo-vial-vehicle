package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PendingProposal es lo que el llamador propone para la fila pendiente de una
// patente sin dueños: el pending_ref a usar si hay que crear una, el instante
// de referencia y el vencimiento que se fija o extiende. Si la patente ya tiene
// una fila vigente se conserva su pending_ref y solo se extiende el vencimiento.
type PendingProposal struct {
	Ref       uuid.UUID
	Now       time.Time
	ExpiresAt time.Time
}

// VehicleRepository define la persistencia de vehículos requerida por el dominio.
// La violación de la restricción de unicidad (owner_id, pat_hashed) debe traducirse
// por la implementación concreta en ErrDuplicateVehicle.
type VehicleRepository interface {
	// Create vincula el vehículo al titular y retorna cuántos vehículos tiene
	// vinculados después del alta. El control del tope (maxPerOwner) y el alta van
	// en la misma transacción, serializada por titular, para que dos altas
	// simultáneas no lo superen. Retorna ErrDuplicateVehicle si el titular ya
	// tiene esa patente y ErrVehicleLimitReached si ya tenía maxPerOwner. Si la
	// patente tenía alertas pendientes (US-10), la misma transacción las reclama.
	Create(ctx context.Context, vehicle *Vehicle, maxPerOwner int) (int, error)

	// CountByOwner retorna cuántos vehículos tiene vinculados el titular.
	CountByOwner(ctx context.Context, ownerID uuid.UUID) (int, error)

	// DeleteByOwnerAndPlate desvincula la patente (por blind index) del titular y
	// retorna cuántos vehículos le quedan. Retorna ErrVehicleNotFound si el
	// titular no la tiene vinculada; el vínculo de otro titular no se toca. No
	// emite eventos.
	DeleteByOwnerAndPlate(ctx context.Context, ownerID uuid.UUID, patHashed string) (int, error)

	// DeleteByOwner desvincula todos los vehículos del titular y retorna cuántos
	// borró. Es idempotente.
	DeleteByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)

	// FindOwnersOrReservePending retorna los owner_id de todos los vehículos
	// vinculados con el Blind Index dado (relación 1 patente -> N dueños) y, si
	// no hay ninguno, deja la patente reservada como pendiente de entrega
	// (US-10) y retorna su pending_ref. Es atómica frente a una vinculación
	// concurrente de la misma patente: o ve al nuevo dueño o la vinculación ve
	// la reserva. Con dueños, el pending_ref es nil. Un slice vacío es un
	// resultado válido, no un error (/internal/resolve-owner).
	FindOwnersOrReservePending(ctx context.Context, patHashed string, proposal PendingProposal) ([]uuid.UUID, *uuid.UUID, error)

	// DeleteExpiredPending borra las patentes pendientes vencidas a now y
	// retorna cuántas borró.
	DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error)
}
