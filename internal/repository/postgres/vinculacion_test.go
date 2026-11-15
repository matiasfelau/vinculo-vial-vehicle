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

// vincular crea un vehículo del titular con una patente nueva y retorna su blind
// index, con el tope dado.
func vincular(t *testing.T, repo *VehicleRepository, ownerID uuid.UUID, maxPerOwner int) string {
	t.Helper()

	patHashed := uuid.NewString()
	if _, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, patHashed, time.Now().UTC()), maxPerOwner); err != nil {
		t.Fatalf("no se pudo vincular el vehículo: %v", err)
	}
	return patHashed
}

func vehiculosDelTitular(t *testing.T, db *gorm.DB, ownerID uuid.UUID) int64 {
	t.Helper()

	var total int64
	if err := db.Model(&vehicleModel{}).Where("owner_id = ?", ownerID).Count(&total).Error; err != nil {
		t.Fatalf("no se pudo contar los vehículos: %v", err)
	}
	return total
}

func eventosDeOutbox(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var total int64
	if err := db.Model(&outboxEventModel{}).Count(&total).Error; err != nil {
		t.Fatalf("no se pudo contar el outbox: %v", err)
	}
	return total
}

func TestCreateRetornaCuantosVehiculosTieneElTitularDespuesDelAlta(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID := uuid.New()

	for esperado := 1; esperado <= 3; esperado++ {
		total, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3)
		if err != nil {
			t.Fatalf("alta %d: no se esperaba error: %v", esperado, err)
		}
		if total != esperado {
			t.Fatalf("alta %d: conteo = %d", esperado, total)
		}
	}
}

func TestCreateEnElTopeRetornaVehicleLimitReachedYNoInserta(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	for i := 0; i < 3; i++ {
		vincular(t, repo, ownerID, 3)
	}

	total, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3)

	if !errors.Is(err, domain.ErrVehicleLimitReached) {
		t.Fatalf("esperaba domain.ErrVehicleLimitReached, obtuvo %v", err)
	}
	if total != 0 {
		t.Fatalf("un alta rechazada no debe devolver conteo, obtuvo %d", total)
	}
	if n := vehiculosDelTitular(t, db, ownerID); n != 3 {
		t.Fatalf("el titular tiene %d vehículos, esperaba 3", n)
	}
}

func TestCreateRepetirUnaPatenteEnElTopeRetornaDuplicadoYNoTope(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID := uuid.New()
	vincular(t, repo, ownerID, 3)
	vincular(t, repo, ownerID, 3)
	repetida := vincular(t, repo, ownerID, 3)

	_, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, repetida, time.Now().UTC()), 3)

	if !errors.Is(err, domain.ErrDuplicateVehicle) {
		t.Fatalf("esperaba domain.ErrDuplicateVehicle, obtuvo %v", err)
	}
}

func TestCreateElTopeCuentaSoloLosVehiculosPropios(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	otro := uuid.New()
	for i := 0; i < 3; i++ {
		vincular(t, repo, otro, 3)
	}

	total, err := repo.Create(context.Background(), nuevoVehiculo(uuid.New(), uuid.NewString(), time.Now().UTC()), 3)

	if err != nil || total != 1 {
		t.Fatalf("los vehículos de otro titular no cuentan para el tope: total=%d err=%v", total, err)
	}
}

func TestCreateElTopeEsElQueSePasaPorParametro(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID := uuid.New()
	vincular(t, repo, ownerID, 1)

	_, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 1)

	if !errors.Is(err, domain.ErrVehicleLimitReached) {
		t.Fatalf("con tope 1 el segundo alta debía rechazarse, obtuvo %v", err)
	}
}

// Un alta rechazada por el tope no puede consumir la reserva pendiente de la
// patente: las alertas esperan a quien sí pueda vincularla.
func TestCreateRechazadaPorElTopeNoReclamaLaPatentePendiente(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	for i := 0; i < 3; i++ {
		vincular(t, repo, ownerID, 3)
	}
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()
	reservar(t, repo, patHashed, ahora)
	eventosAntes := eventosDeOutbox(t, db)

	_, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, patHashed, ahora), 3)

	if !errors.Is(err, domain.ErrVehicleLimitReached) {
		t.Fatalf("esperaba domain.ErrVehicleLimitReached, obtuvo %v", err)
	}
	if _, existe := filaPendiente(t, db, patHashed); !existe {
		t.Fatal("la reserva pendiente debía conservarse")
	}
	if n := eventosDeOutbox(t, db); n != eventosAntes {
		t.Fatalf("no debía encolarse ningún evento, el outbox pasó de %d a %d", eventosAntes, n)
	}
}

// Dos altas simultáneas de un titular que ya tiene 2 no pueden terminar con 4: el
// lock por titular las serializa y solo una pasa el tope.
func TestCreateDosAltasConcurrentesNoSuperanElTope(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	for ronda := 0; ronda < 20; ronda++ {
		ownerID := uuid.New()
		vincular(t, repo, ownerID, 3)
		vincular(t, repo, ownerID, 3)

		resultados := altasConcurrentes(repo, ownerID, 2, 3)

		if exitos, topes := contar(resultados); exitos != 1 || topes != 1 {
			t.Fatalf("ronda %d: esperaba 1 alta y 1 rechazo por tope, obtuvo %d y %d (%v)", ronda, exitos, topes, resultados)
		}
		if n := vehiculosDelTitular(t, db, ownerID); n != 3 {
			t.Fatalf("ronda %d: el titular terminó con %d vehículos, esperaba 3", ronda, n)
		}
	}
}

func TestCreateMuchasAltasConcurrentesDesdeCeroTerminanEnElTope(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()

	resultados := altasConcurrentes(repo, ownerID, 8, 3)

	exitos, topes := contar(resultados)
	if exitos != 3 || topes != 5 {
		t.Fatalf("esperaba 3 altas y 5 rechazos por tope, obtuvo %d y %d (%v)", exitos, topes, resultados)
	}
	if n := vehiculosDelTitular(t, db, ownerID); n != 3 {
		t.Fatalf("el titular terminó con %d vehículos, esperaba 3", n)
	}
}

func TestCreateLaMismaPatenteConcurrenteDelMismoTitularSeVinculaUnaSolaVez(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	patHashed := uuid.NewString()

	resultados := make([]error, 4)
	var listos, salida sync.WaitGroup
	arranque := make(chan struct{})
	listos.Add(len(resultados))
	salida.Add(len(resultados))
	for i := range resultados {
		go func() {
			defer salida.Done()
			listos.Done()
			<-arranque
			_, resultados[i] = repo.Create(context.Background(), nuevoVehiculo(ownerID, patHashed, time.Now().UTC()), 3)
		}()
	}
	listos.Wait()
	close(arranque)
	salida.Wait()

	exitos, duplicados := 0, 0
	for _, err := range resultados {
		switch {
		case err == nil:
			exitos++
		case errors.Is(err, domain.ErrDuplicateVehicle):
			duplicados++
		default:
			t.Fatalf("error inesperado: %v", err)
		}
	}
	if exitos != 1 || duplicados != 3 {
		t.Fatalf("esperaba 1 alta y 3 duplicados, obtuvo %d y %d", exitos, duplicados)
	}
	if n := vehiculosDelTitular(t, db, ownerID); n != 1 {
		t.Fatalf("el titular terminó con %d vehículos, esperaba 1", n)
	}
}

// altasConcurrentes lanza n altas de patentes distintas del mismo titular, todas a
// la vez, y retorna el error de cada una.
func altasConcurrentes(repo *VehicleRepository, ownerID uuid.UUID, n, maxPerOwner int) []error {
	resultados := make([]error, n)
	var listos, salida sync.WaitGroup
	arranque := make(chan struct{})
	listos.Add(n)
	salida.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer salida.Done()
			listos.Done()
			<-arranque
			_, resultados[i] = repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), maxPerOwner)
		}()
	}
	listos.Wait()
	close(arranque)
	salida.Wait()
	return resultados
}

func contar(resultados []error) (exitos, topes int) {
	for _, err := range resultados {
		switch {
		case err == nil:
			exitos++
		case errors.Is(err, domain.ErrVehicleLimitReached):
			topes++
		}
	}
	return exitos, topes
}

func TestCountByOwnerCuentaSoloLosVehiculosDelTitular(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID, otro := uuid.New(), uuid.New()
	vincular(t, repo, ownerID, 3)
	vincular(t, repo, ownerID, 3)
	vincular(t, repo, otro, 3)

	propio, err := repo.CountByOwner(context.Background(), ownerID)
	if err != nil || propio != 2 {
		t.Fatalf("el titular tiene 2 vehículos: conteo=%d err=%v", propio, err)
	}
	ninguno, err := repo.CountByOwner(context.Background(), uuid.New())
	if err != nil || ninguno != 0 {
		t.Fatalf("un titular sin vehículos tiene conteo 0: conteo=%d err=%v", ninguno, err)
	}
}

func TestDeleteByOwnerAndPlateDesvinculaLaPatenteYRetornaLosQueQuedan(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	objetivo := vincular(t, repo, ownerID, 3)
	conservada := vincular(t, repo, ownerID, 3)

	restantes, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, objetivo)

	if err != nil || restantes != 1 {
		t.Fatalf("esperaba quedar con 1 vehículo: restantes=%d err=%v", restantes, err)
	}
	var quedan []vehicleModel
	if err := db.Where("owner_id = ?", ownerID).Find(&quedan).Error; err != nil {
		t.Fatal(err)
	}
	if len(quedan) != 1 || quedan[0].PatHashed != conservada {
		t.Fatalf("debía conservarse solo la otra patente, quedó %+v", quedan)
	}
}

func TestDeleteByOwnerAndPlateDeUnaPatenteNoVinculadaRetornaNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	vincular(t, repo, ownerID, 3)

	_, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, uuid.NewString())

	if !errors.Is(err, domain.ErrVehicleNotFound) {
		t.Fatalf("esperaba domain.ErrVehicleNotFound, obtuvo %v", err)
	}
	if n := vehiculosDelTitular(t, db, ownerID); n != 1 {
		t.Fatalf("no debía borrarse nada, el titular tiene %d", n)
	}
}

// La patente que tiene vinculada otro usuario no se puede desvincular: responde
// NOT_FOUND y el vínculo ajeno sigue.
func TestDeleteByOwnerAndPlateDeLaPatenteDeOtroTitularRetornaNotFoundYNoLaToca(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID, otro := uuid.New(), uuid.New()
	compartida := vincular(t, repo, otro, 3)
	vincular(t, repo, ownerID, 3)

	_, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, compartida)

	if !errors.Is(err, domain.ErrVehicleNotFound) {
		t.Fatalf("esperaba domain.ErrVehicleNotFound, obtuvo %v", err)
	}
	if n := vehiculosDelTitular(t, db, otro); n != 1 {
		t.Fatalf("el vínculo del otro titular debía seguir, tiene %d", n)
	}
	if n := vehiculosDelTitular(t, db, ownerID); n != 1 {
		t.Fatalf("los vehículos propios no se tocan, tiene %d", n)
	}
}

// Dos titulares con la misma patente: si uno la desvincula, el otro la conserva.
func TestDeleteByOwnerAndPlateConservaElVinculoDelOtroTitularDeLaMismaPatente(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	uno, otro := uuid.New(), uuid.New()
	for _, owner := range []uuid.UUID{uno, otro} {
		if _, err := repo.Create(context.Background(), nuevoVehiculo(owner, patHashed, time.Now().UTC()), 3); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := repo.DeleteByOwnerAndPlate(context.Background(), uno, patHashed); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	owners, _, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(time.Now().UTC()))
	if err != nil || len(owners) != 1 || owners[0] != otro {
		t.Fatalf("la patente debía seguir resolviendo solo al otro titular: owners=%v err=%v", owners, err)
	}
}

func TestDeleteByOwnerAndPlateLiberaElLugarDeInmediato(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID := uuid.New()
	primera := vincular(t, repo, ownerID, 3)
	vincular(t, repo, ownerID, 3)
	vincular(t, repo, ownerID, 3)
	if _, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3); !errors.Is(err, domain.ErrVehicleLimitReached) {
		t.Fatalf("el titular debía estar en el tope: %v", err)
	}

	if _, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, primera); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if _, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3); err != nil {
		t.Fatalf("tras desvincular debía poder vincular otra: %v", err)
	}
}

// Desvincular no emite eventos ni toca la reserva de alertas pendientes.
func TestDeleteByOwnerAndPlateNoEmiteEventosNiTocaLasPendientes(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID := uuid.New()
	patHashed := vincular(t, repo, ownerID, 3)
	otraPendiente := uuid.NewString()
	reservar(t, repo, otraPendiente, time.Now().UTC())
	eventosAntes := eventosDeOutbox(t, db)

	if _, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, patHashed); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if n := eventosDeOutbox(t, db); n != eventosAntes {
		t.Fatalf("desvincular no emite eventos: el outbox pasó de %d a %d", eventosAntes, n)
	}
	if _, existe := filaPendiente(t, db, otraPendiente); !existe {
		t.Fatal("desvincular no debía tocar las patentes pendientes")
	}
}

// Tras desvincular, resolve-owner ya no encuentra al titular para esa patente.
func TestDespuesDeDesvincularLaPatenteQuedaSinDuenos(t *testing.T) {
	repo := NewVehicleRepository(setupTestDB(t))
	ownerID := uuid.New()
	patHashed := vincular(t, repo, ownerID, 3)

	if _, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, patHashed); err != nil {
		t.Fatal(err)
	}

	owners, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(time.Now().UTC()))
	if err != nil || len(owners) != 0 || ref == nil {
		t.Fatalf("la patente debía quedar sin dueños y pendiente: owners=%v ref=%v err=%v", owners, ref, err)
	}
}

func TestVehiculosDeUnTitularSeDesvinculanTodosYSoloLosPropios(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ownerID, otro := uuid.New(), uuid.New()
	for i := 0; i < 3; i++ {
		vincular(t, repo, ownerID, 3)
	}
	vincular(t, repo, otro, 3)

	borrados, err := repo.DeleteByOwner(context.Background(), ownerID)

	if err != nil || borrados != 3 {
		t.Fatalf("esperaba borrar 3: borrados=%d err=%v", borrados, err)
	}
	if n := vehiculosDelTitular(t, db, otro); n != 1 {
		t.Fatalf("el otro titular debía conservar su vehículo, tiene %d", n)
	}
	// Con los lugares liberados puede volver a vincular.
	if _, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3); err != nil {
		t.Fatalf("tras desvincular todos debía poder vincular: %v", err)
	}
}

func TestLasOperacionesDeVinculacionSeClasificanComoIndisponibilidadSiLaBaseTardaDemasiado(t *testing.T) {
	db := setupTestDB(t)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	lockConn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	tx, err := lockConn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(context.Background(), "LOCK TABLE vehicles IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("no se pudo tomar el lock exclusivo: %v", err)
	}

	repo := NewVehicleRepository(db, WithQueryTimeout(50*time.Millisecond))
	ownerID := uuid.New()

	operaciones := map[string]func() error{
		"Create": func() error {
			_, err := repo.Create(context.Background(), nuevoVehiculo(ownerID, uuid.NewString(), time.Now().UTC()), 3)
			return err
		},
		"CountByOwner": func() error {
			_, err := repo.CountByOwner(context.Background(), ownerID)
			return err
		},
		"DeleteByOwnerAndPlate": func() error {
			_, err := repo.DeleteByOwnerAndPlate(context.Background(), ownerID, uuid.NewString())
			return err
		},
	}
	for nombre, operacion := range operaciones {
		if err := operacion(); !errors.Is(err, domain.ErrRepositoryUnavailable) {
			t.Fatalf("%s: esperaba domain.ErrRepositoryUnavailable ante el lock retenido, obtuvo %v", nombre, err)
		}
	}
}
