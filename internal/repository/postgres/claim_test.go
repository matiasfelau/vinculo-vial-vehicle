package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func nuevoVehiculo(ownerID uuid.UUID, patHashed string, createdAt time.Time) *domain.Vehicle {
	return &domain.Vehicle{ID: uuid.New(), OwnerID: ownerID, PatHashed: patHashed, PatAES: "cifrado", CreatedAt: createdAt}
}

func eventosDeLaPatente(t *testing.T, db *gorm.DB, pendingRef uuid.UUID) []outboxEventModel {
	t.Helper()

	var rows []outboxEventModel
	err := db.Where("event_type = ? AND payload->>'pending_ref' = ?", domain.EventTypePendingClaimed, pendingRef.String()).Find(&rows).Error
	if err != nil {
		t.Fatalf("no se pudo leer el outbox: %v", err)
	}
	return rows
}

func reservar(t *testing.T, repo *VehicleRepository, patHashed string, now time.Time) uuid.UUID {
	t.Helper()

	_, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(now))
	if err != nil || ref == nil {
		t.Fatalf("no se pudo reservar la patente: ref=%v err=%v", ref, err)
	}
	return *ref
}

func TestCreatePrimerDuenoConPendienteVigenteBorraLaFilaYEncolaElEvento(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ownerID := uuid.New()
	ahora := time.Now().UTC()
	ref := reservar(t, repo, patHashed, ahora)

	if err := crear(repo, nuevoVehiculo(ownerID, patHashed, ahora)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if _, existe := filaPendiente(t, db, patHashed); existe {
		t.Fatal("la fila pendiente debía borrarse al vincular")
	}
	eventos := eventosDeLaPatente(t, db, ref)
	if len(eventos) != 1 {
		t.Fatalf("esperaba un único evento, obtuvo %d", len(eventos))
	}
	evento := eventos[0]
	if evento.Actor == nil || *evento.Actor != ownerID {
		t.Fatalf("actor = %v, esperaba el owner_id %v", evento.Actor, ownerID)
	}
	if evento.Status != outboxStatusPending {
		t.Fatalf("status = %q, esperaba pending", evento.Status)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(evento.Payload), &payload); err != nil {
		t.Fatalf("payload inválido: %v", err)
	}
	if len(payload) != 1 || payload["pending_ref"] != ref.String() {
		t.Fatalf("el payload debía ser solo {pending_ref}, obtuvo %v", payload)
	}
}

func TestCreateSinPendienteNoEncolaNingunEvento(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	if err := crear(repo, nuevoVehiculo(uuid.New(), uuid.NewString(), time.Now().UTC())); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	var eventos int64
	if err := db.Model(&outboxEventModel{}).Count(&eventos).Error; err != nil {
		t.Fatalf("no se pudo contar el outbox: %v", err)
	}
	if eventos != 0 {
		t.Fatalf("sin reserva no debía encolarse ningún evento, hay %d", eventos)
	}
}

func TestCreateConPendienteVencidoBorraLaFilaSinEvento(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()
	ref := reservar(t, repo, patHashed, ahora.Add(-2*ventanaDePrueba))

	if err := crear(repo, nuevoVehiculo(uuid.New(), patHashed, ahora)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if _, existe := filaPendiente(t, db, patHashed); existe {
		t.Fatal("la fila vencida también debía borrarse")
	}
	if eventos := eventosDeLaPatente(t, db, ref); len(eventos) != 0 {
		t.Fatalf("una reserva vencida no debía generar evento, generó %d", len(eventos))
	}
}

func TestCreateDuplicadoRevierteLaTransaccionYConservaLaReserva(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ownerID := uuid.New()
	ahora := time.Now().UTC()
	// Estado anómalo, armado a mano: el dueño ya tiene la patente y aun así hay una
	// reserva. Un Create duplicado debe fallar sin tocarla.
	crearVehiculo(t, repo, ownerID, patHashed)
	ref := uuid.New()
	if err := db.Create(&pendingPlateModel{PlateHMAC: patHashed, PendingRef: ref, ExpiresAt: ahora.Add(ventanaDePrueba), CreatedAt: ahora}).Error; err != nil {
		t.Fatalf("no se pudo armar la reserva: %v", err)
	}

	err := crear(repo, nuevoVehiculo(ownerID, patHashed, ahora))

	if !errors.Is(err, domain.ErrDuplicateVehicle) {
		t.Fatalf("esperaba domain.ErrDuplicateVehicle, obtuvo %v", err)
	}
	if _, existe := filaPendiente(t, db, patHashed); !existe {
		t.Fatal("al revertirse la transacción la reserva debía conservarse")
	}
	if eventos := eventosDeLaPatente(t, db, ref); len(eventos) != 0 {
		t.Fatalf("una vinculación fallida no debía encolar eventos, encoló %d", len(eventos))
	}
}

func TestCreateFalloAlEncolarRevierteElVehiculo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()
	reservar(t, repo, patHashed, ahora)
	// Sin la tabla de outbox el evento no puede encolarse: el vehículo tampoco
	// debe quedar creado.
	if err := db.Exec("ALTER TABLE outbox_events RENAME TO outbox_events_oculta").Error; err != nil {
		t.Fatalf("no se pudo ocultar el outbox: %v", err)
	}
	restaurado := false
	restaurar := func() {
		if !restaurado {
			restaurado = true
			db.Exec("ALTER TABLE outbox_events_oculta RENAME TO outbox_events")
		}
	}
	t.Cleanup(restaurar)

	err := crear(repo, nuevoVehiculo(uuid.New(), patHashed, ahora))
	restaurar()

	if err == nil {
		t.Fatal("esperaba un error al no poder encolar el evento")
	}
	var vehiculos int64
	if err := db.Model(&vehicleModel{}).Where("pat_hashed = ?", patHashed).Count(&vehiculos).Error; err != nil {
		t.Fatalf("no se pudo contar vehicles: %v", err)
	}
	if vehiculos != 0 {
		t.Fatal("el vehículo no debía quedar creado si el evento no se pudo encolar")
	}
	if _, existe := filaPendiente(t, db, patHashed); !existe {
		t.Fatal("la reserva debía conservarse al revertirse la transacción")
	}
}

// Dos vinculaciones simultáneas de la misma patente sin dueño generan un único
// evento: la primera consume la reserva y la segunda ya no la encuentra.
func TestCreateConcurrenteDeDosDuenosGeneraUnUnicoEvento(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db, WithQueryTimeout(10*time.Second))
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()
	ref := reservar(t, repo, patHashed, ahora)
	const duenos = 6

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	inicio := make(chan struct{})
	for i := 0; i < duenos; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-inicio
			if err := crear(repo, nuevoVehiculo(uuid.New(), patHashed, time.Now().UTC())); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	close(inicio)
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("no se esperaban errores: %v", errs)
	}
	if eventos := eventosDeLaPatente(t, db, ref); len(eventos) != 1 {
		t.Fatalf("esperaba un único evento entre %d vinculaciones simultáneas, obtuvo %d", duenos, len(eventos))
	}
	var vehiculos int64
	if err := db.Model(&vehicleModel{}).Where("pat_hashed = ?", patHashed).Count(&vehiculos).Error; err != nil {
		t.Fatalf("no se pudo contar vehicles: %v", err)
	}
	if vehiculos != duenos {
		t.Fatalf("esperaba %d vehículos vinculados, hay %d", duenos, vehiculos)
	}
}

// Una consulta de dueños y una vinculación simultáneas a la misma patente no
// pueden dejar un dueño con una reserva huérfana: o la consulta ve al dueño, o la
// vinculación consume la reserva y encola el evento.
func TestResolveYCreateConcurrentesNoDejanReservaHuerfana(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db, WithQueryTimeout(10*time.Second))

	for i := 0; i < 25; i++ {
		patHashed := uuid.NewString()
		var (
			wg         sync.WaitGroup
			resolveErr error
			createErr  error
			pendingRef *uuid.UUID
		)
		inicio := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-inicio
			_, pendingRef, resolveErr = repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(time.Now().UTC()))
		}()
		go func() {
			defer wg.Done()
			<-inicio
			createErr = crear(repo, nuevoVehiculo(uuid.New(), patHashed, time.Now().UTC()))
		}()
		close(inicio)
		wg.Wait()

		if resolveErr != nil || createErr != nil {
			t.Fatalf("iteración %d: resolve=%v create=%v", i, resolveErr, createErr)
		}
		if _, existe := filaPendiente(t, db, patHashed); existe {
			t.Fatalf("iteración %d: quedó una reserva huérfana para una patente con dueño", i)
		}
		if pendingRef != nil {
			// La consulta reservó antes de la vinculación: el dueño tiene que haber
			// consumido la reserva y encolado el evento.
			if eventos := eventosDeLaPatente(t, db, *pendingRef); len(eventos) != 1 {
				t.Fatalf("iteración %d: esperaba 1 evento para la reserva, obtuvo %d", i, len(eventos))
			}
		}
	}
}
