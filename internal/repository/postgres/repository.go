package postgres

import (
	"context"
	"errors"
	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// uniqueViolationCode es el código SQLSTATE que PostgreSQL retorna ante la
// violación de una restricción unique.
const uniqueViolationCode = "23505"

// classifyError distingue una indisponibilidad de infraestructura (la base de
// datos es inalcanzable, o la consulta excedió su plazo) de un error propio de
// la aplicación. Solo la primera categoría se envuelve en
// domain.ErrRepositoryUnavailable, que los handlers HTTP mapean a 503: es una
// falla de la dependencia, no un defecto del servicio.
func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// El plazo de la consulta (context.WithTimeout, ver Connect/repositorio) o el
	// contexto de la solicitud entrante se agotaron antes de completarse.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errors.Join(domain.ErrRepositoryUnavailable, err)
	}

	// Errores de red (conexión rechazada, timeout de socket, etc.) al dialogar con
	// la base de datos, sin llegar a obtener una respuesta del servidor SQL.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return errors.Join(domain.ErrRepositoryUnavailable, err)
	}

	return err
}

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
		return classifyError(err)
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
		return false, classifyError(err)
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
		return nil, classifyError(err)
	}
	return ownerIDs, nil
}
