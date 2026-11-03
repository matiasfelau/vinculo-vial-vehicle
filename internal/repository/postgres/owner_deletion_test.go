package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func contarVehiculos(t *testing.T, db *gorm.DB, ownerID uuid.UUID) int64 {
	t.Helper()

	var cantidad int64
	if err := db.Model(&vehicleModel{}).Where("owner_id = ?", ownerID).Count(&cantidad).Error; err != nil {
		t.Fatalf("no se pudo contar los vehículos: %v", err)
	}
	return cantidad
}

func TestDeleteByOwnerBorraTodosLosVehiculosDelTitular(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	for i := 0; i < 3; i++ {
		crearVehiculo(t, repo, ownerID, uuid.NewString())
	}

	borrados, err := repo.DeleteByOwner(context.Background(), ownerID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if borrados != 3 {
		t.Errorf("borrados = %d, esperaba 3", borrados)
	}
	if quedan := contarVehiculos(t, db, ownerID); quedan != 0 {
		t.Errorf("quedaron %d vehículos del titular dado de baja", quedan)
	}
}

func TestDeleteByOwnerConservaLosVehiculosDeOtrosTitulares(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	baja, otro := uuid.New(), uuid.New()
	crearVehiculo(t, repo, baja, uuid.NewString())
	crearVehiculo(t, repo, otro, uuid.NewString())
	crearVehiculo(t, repo, otro, uuid.NewString())

	if _, err := repo.DeleteByOwner(context.Background(), baja); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if quedan := contarVehiculos(t, db, otro); quedan != 2 {
		t.Errorf("el otro titular tiene %d vehículos, esperaba 2", quedan)
	}
}

// 1 patente -> N dueños: la baja de uno no desvincula a los demás de esa patente.
func TestDeleteByOwnerConPatenteCompartidaElOtroTitularConservaSuVinculo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	baja, otro := uuid.New(), uuid.New()
	compartida := uuid.NewString()
	crearVehiculo(t, repo, baja, compartida)
	crearVehiculo(t, repo, otro, compartida)

	borrados, err := repo.DeleteByOwner(context.Background(), baja)

	if err != nil || borrados != 1 {
		t.Fatalf("borrados = %d, error = %v; esperaba 1 sin error", borrados, err)
	}
	duenos, pendingRef, err := repo.FindOwnersOrReservePending(context.Background(), compartida, propuesta(time.Now().UTC()))
	if err != nil {
		t.Fatalf("no se pudo resolver la patente compartida: %v", err)
	}
	if len(duenos) != 1 || duenos[0] != otro {
		t.Errorf("dueños de la patente = %v, esperaba solo %v", duenos, otro)
	}
	if pendingRef != nil {
		t.Error("la patente sigue teniendo dueño: no debía reservarse como pendiente")
	}
}

func TestDeleteByOwnerEsIdempotente(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	baja, otro := uuid.New(), uuid.New()
	compartida := uuid.NewString()
	crearVehiculo(t, repo, baja, compartida)
	crearVehiculo(t, repo, baja, uuid.NewString())
	crearVehiculo(t, repo, otro, compartida)

	primera, err := repo.DeleteByOwner(context.Background(), baja)
	if err != nil || primera != 2 {
		t.Fatalf("primera entrega: borrados = %d, error = %v; esperaba 2 sin error", primera, err)
	}

	for entrega := 2; entrega <= 3; entrega++ {
		borrados, err := repo.DeleteByOwner(context.Background(), baja)
		if err != nil || borrados != 0 {
			t.Fatalf("entrega %d: borrados = %d, error = %v; esperaba 0 sin error", entrega, borrados, err)
		}
	}

	if quedan := contarVehiculos(t, db, otro); quedan != 1 {
		t.Errorf("el otro titular tiene %d vehículos, esperaba 1", quedan)
	}
}

func TestDeleteByOwnerSinVehiculosRetornaCeroSinError(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))

	borrados, err := repo.DeleteByOwner(context.Background(), uuid.New())

	if err != nil || borrados != 0 {
		t.Fatalf("borrados = %d, error = %v; esperaba 0 sin error", borrados, err)
	}
}

// Dos entregas del mismo evento pueden correr a la vez (dos instancias, o una
// reentrega durante un reintento): entre las dos borran cada vehículo una vez.
func TestDeleteByOwnerConcurrenteBorraCadaVehiculoUnaSolaVez(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	const vehiculos = 5
	for i := 0; i < vehiculos; i++ {
		crearVehiculo(t, repo, ownerID, uuid.NewString())
	}

	const entregas = 4
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		total    int64
		primerEr error
	)
	for i := 0; i < entregas; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			borrados, err := repo.DeleteByOwner(context.Background(), ownerID)
			mu.Lock()
			defer mu.Unlock()
			total += borrados
			if err != nil && primerEr == nil {
				primerEr = err
			}
		}()
	}
	wg.Wait()

	if primerEr != nil {
		t.Fatalf("ninguna entrega debía fallar: %v", primerEr)
	}
	if total != vehiculos {
		t.Errorf("entre todas borraron %d vehículos, esperaba %d", total, vehiculos)
	}
	if quedan := contarVehiculos(t, db, ownerID); quedan != 0 {
		t.Errorf("quedaron %d vehículos", quedan)
	}
}

// La baja no toca pending_plates: la reserva de una patente sin dueños es
// independiente de quién vinculó otras patentes, y quedarse sin dueños no
// reserva la patente por sí solo.
func TestDeleteByOwnerNoTocaPendingPlates(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ahora := time.Now().UTC()
	baja := uuid.New()
	unica := uuid.NewString()
	crearVehiculo(t, repo, baja, unica)
	reservada := uuid.NewString()
	ref := reservar(t, repo, reservada, ahora)

	if _, err := repo.DeleteByOwner(context.Background(), baja); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	fila, existe := filaPendiente(t, db, reservada)
	if !existe || fila.PendingRef != ref {
		t.Errorf("la reserva existente debía quedar intacta: existe=%v fila=%+v", existe, fila)
	}
	if _, existe := filaPendiente(t, db, unica); existe {
		t.Error("la baja no debía reservar la patente que se quedó sin dueños")
	}
}

// Un fallo de la base (aquí, un lock retenido que agota el plazo de la consulta)
// se informa como indisponibilidad: el consumidor lo trata como transitorio y
// reintenta, en lugar de descartar la baja.
func TestDeleteByOwnerCortaAntesDeQueElLockSeLibere(t *testing.T) {
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
	_, err = repo.DeleteByOwner(context.Background(), uuid.New())
	transcurrido := time.Since(inicio)

	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable ante el lock retenido, obtuvo %v", err)
	}
	if transcurrido > 500*time.Millisecond {
		t.Fatalf("esperaba que el plazo de 50ms cortara la consulta con prontitud; transcurrido=%v", transcurrido)
	}
}
