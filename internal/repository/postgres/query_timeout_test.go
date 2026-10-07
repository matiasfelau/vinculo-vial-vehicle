package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// TestConsultaQueExcedeElPlazoSeClasificaComoIndisponibilidad reproduce, contra
// una instancia real de PostgreSQL, el mecanismo que sostiene S1-D3: una
// consulta que excede el contexto (aquí forzada con pg_sleep) retorna un error
// envuelto en context.DeadlineExceeded, que classifyError reclasifica como
// domain.ErrRepositoryUnavailable. Es el mismo error que produciría el plazo
// por consulta de WithQueryTimeout ante una base de datos lenta o bloqueada.
func TestConsultaQueExcedeElPlazoSeClasificaComoIndisponibilidad(t *testing.T) {
	db := setupTestDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := db.WithContext(ctx).Exec("SELECT pg_sleep(0.5)").Error
	if err == nil {
		t.Fatal("esperaba que la consulta fallara al exceder el plazo del contexto")
	}

	clasificado := classifyError(err)
	if !errors.Is(clasificado, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", clasificado)
	}
}

// TestFindOwnersByPatHashCortaAntesDeQueElLockSeLibere retiene un lock exclusivo
// sobre vehicles desde una segunda conexión, sin liberarlo, para verificar que
// el plazo por consulta de WithQueryTimeout corta FindOwnersByPatHash en lugar de
// esperar indefinidamente a que el lock se libere.
func TestFindOwnersByPatHashCortaAntesDeQueElLockSeLibere(t *testing.T) {
	db := setupTestDB(t)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("no se pudo obtener la conexión subyacente: %v", err)
	}

	lockConn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("no se pudo abrir una segunda conexión para el lock: %v", err)
	}
	defer lockConn.Close()

	tx, err := lockConn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("no se pudo iniciar la transacción del lock: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(context.Background(), "LOCK TABLE vehicles IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("no se pudo tomar el lock exclusivo: %v", err)
	}

	repo := NewVehicleRepository(db, WithQueryTimeout(50*time.Millisecond))

	inicio := time.Now()
	_, err = repo.FindOwnersByPatHash(context.Background(), "no-importa")
	transcurrido := time.Since(inicio)

	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable ante el lock retenido, obtuvo %v", err)
	}
	if transcurrido > 500*time.Millisecond {
		t.Fatalf("esperaba que el plazo de 50ms cortara la consulta con prontitud; transcurrido=%v", transcurrido)
	}
}
