package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// uniqueViolationCode es el código SQLSTATE que PostgreSQL retorna ante la
// violación de una restricción unique.
const uniqueViolationCode = "23505"

// VehicleRepository implementa domain.VehicleRepository sobre PostgreSQL vía GORM.
type VehicleRepository struct {
	db *gorm.DB
}

// NewVehicleRepository construye un VehicleRepository sobre la conexión dada.
func NewVehicleRepository(db *gorm.DB) *VehicleRepository {
	return &VehicleRepository{db: db}
}

// Create persiste el vehículo. Ante la violación de UNIQUE(owner_id, pat_hashed)
// retorna domain.ErrDuplicateVehicle.
func (r *VehicleRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	model := vehicleModel{
		ID:        vehicle.ID,
		OwnerID:   vehicle.OwnerID,
		PatHashed: vehicle.PatHashed,
		PatAES:    vehicle.PatAES,
		CreatedAt: vehicle.CreatedAt,
	}

	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return domain.ErrDuplicateVehicle
		}
		return err
	}

	return nil
}

// ExistsByPatHash indica si existe al menos un vehículo con ese Blind Index.
func (r *VehicleRepository) ExistsByPatHash(ctx context.Context, patHashed string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).
		Raw("SELECT EXISTS(SELECT 1 FROM vehicles WHERE pat_hashed = ?)", patHashed).
		Scan(&exists).Error
	if err != nil {
		return false, err
	}
	return exists, nil
}

// FindOwnersByPatHash retorna todos los owner_id vinculados a ese Blind Index.
// Un resultado vacío es válido: significa que la patente no está en la red.
func (r *VehicleRepository) FindOwnersByPatHash(ctx context.Context, patHashed string) ([]uuid.UUID, error) {
	var ownerIDs []uuid.UUID
	err := r.db.WithContext(ctx).
		Model(&vehicleModel{}).
		Where("pat_hashed = ?", patHashed).
		Pluck("owner_id", &ownerIDs).Error
	if err != nil {
		return nil, err
	}
	return ownerIDs, nil
}
