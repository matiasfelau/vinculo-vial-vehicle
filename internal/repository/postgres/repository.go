package postgres

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// defaultQueryTimeout acota cada consulta cuando el llamador no especifica un
// valor propio vía WithQueryTimeout. Sin este plazo, una consulta bloqueada
// (lock, réplica caída) retiene la goroutine y la conexión sin límite. Se
// mantiene alineado con el default de DB_QUERY_TIMEOUT_MS (ver pkg/config):
// por debajo del timeout servicio-a-servicio con el que Alert llama a este
// servicio, para no violar la regla de anidamiento de timeouts.
const defaultQueryTimeout = 1500 * time.Millisecond

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

	// Códigos SQLSTATE de indisponibilidad (contractual):
	// 08xxx (conexión), 53xxx (recursos), 57014 (statement_timeout), 57P0x (reinicio/apagado).
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		code := pgErr.Code
		if len(code) >= 2 && (code[:2] == "08" || code[:2] == "53" || code == "57014" || (len(code) >= 4 && code[:4] == "57P0")) {
			return errors.Join(domain.ErrRepositoryUnavailable, err)
		}
	}

	return err
}

// VehicleRepository implementa domain.VehicleRepository sobre PostgreSQL vía GORM.
type VehicleRepository struct {
	db           *gorm.DB
	queryTimeout time.Duration
}

// Option configura aspectos opcionales de VehicleRepository en su construcción.
type Option func(*VehicleRepository)

// WithQueryTimeout fija el plazo máximo de cada consulta. Agotado ese plazo,
// el contexto se cancela con context.DeadlineExceeded, que classifyError
// reclasifica como domain.ErrRepositoryUnavailable (503): es una
// indisponibilidad de la base de datos, no un defecto del servicio.
func WithQueryTimeout(timeout time.Duration) Option {
	return func(r *VehicleRepository) {
		r.queryTimeout = timeout
	}
}

// NewVehicleRepository construye un VehicleRepository sobre la conexión dada.
// Sin WithQueryTimeout, aplica defaultQueryTimeout.
func NewVehicleRepository(db *gorm.DB, opts ...Option) *VehicleRepository {
	r := &VehicleRepository{db: db, queryTimeout: defaultQueryTimeout}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Create persiste el vehículo. Ante la violación de UNIQUE(owner_id, pat_hashed)
// retorna domain.ErrDuplicateVehicle.
//
// Si la patente tenía una reserva pendiente vigente (US-10), es decir, este es el
// primer dueño de una patente con alertas esperando, la misma transacción borra
// la reserva y encola vehicle.pending.claimed en el outbox: el vehículo, el
// borrado y el evento se confirman juntos o ninguno.
func (r *VehicleRepository) Create(ctx context.Context, vehicle *domain.Vehicle) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	model := vehicleModel{
		ID:        vehicle.ID,
		OwnerID:   vehicle.OwnerID,
		PatHashed: vehicle.PatHashed,
		PatAES:    vehicle.PatAES,
		CreatedAt: vehicle.CreatedAt,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPlate(tx, vehicle.PatHashed); err != nil {
			return err
		}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return claimPending(tx, vehicle)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return domain.ErrDuplicateVehicle
		}
		return classifyError(err)
	}

	return nil
}

// Ping verifica la conectividad con la base de datos.
func (r *VehicleRepository) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	db, err := r.db.DB()
	if err != nil {
		return classifyError(err)
	}
	if err := db.PingContext(ctx); err != nil {
		return classifyError(err)
	}
	return nil
}
