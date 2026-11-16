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

const ventanaDePrueba = 24 * time.Hour

func propuesta(now time.Time) domain.PendingProposal {
	return domain.PendingProposal{Ref: uuid.New(), Now: now, ExpiresAt: now.Add(ventanaDePrueba)}
}

func crearVehiculo(t *testing.T, repo *VehicleRepository, ownerID uuid.UUID, patHashed string) {
	t.Helper()

	vehicle := &domain.Vehicle{ID: uuid.New(), OwnerID: ownerID, PatHashed: patHashed, PatAES: "cifrado", CreatedAt: time.Now()}
	if err := crear(repo, vehicle); err != nil {
		t.Fatalf("no se pudo crear el vehículo: %v", err)
	}
}

func filaPendiente(t *testing.T, db *gorm.DB, patHashed string) (pendingPlateModel, bool) {
	t.Helper()

	var rows []pendingPlateModel
	if err := db.Where("plate_hmac = ?", patHashed).Find(&rows).Error; err != nil {
		t.Fatalf("no se pudo leer pending_plates: %v", err)
	}
	if len(rows) == 0 {
		return pendingPlateModel{}, false
	}
	return rows[0], true
}

func TestFindOwnersOrReservePendingRetornaLosTresDuenosSinReservar(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	patHashed := uuid.NewString()
	esperados := make(map[uuid.UUID]bool)
	for i := 0; i < 3; i++ {
		ownerID := uuid.New()
		esperados[ownerID] = true
		crearVehiculo(t, repo, ownerID, patHashed)
	}

	owners, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(time.Now().UTC()))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(owners) != 3 {
		t.Fatalf("esperaba 3 dueños, obtuvo %d", len(owners))
	}
	for _, ownerID := range owners {
		if !esperados[ownerID] {
			t.Fatalf("owner_id inesperado en el resultado: %v", ownerID)
		}
		delete(esperados, ownerID)
	}
	if len(esperados) != 0 {
		t.Fatalf("faltaron owner_id en el resultado: %v", esperados)
	}
	if ref != nil {
		t.Fatal("con dueños no debía devolverse pending_ref")
	}
	if _, existe := filaPendiente(t, db, patHashed); existe {
		t.Fatal("con dueños no debía crearse una fila pendiente")
	}
}

func TestFindOwnersOrReservePendingSinDuenosCreaLaFilaPendiente(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()
	p := propuesta(ahora)

	owners, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, p)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(owners) != 0 {
		t.Fatalf("esperaba un slice vacío, obtuvo %d elementos", len(owners))
	}
	if ref == nil || *ref != p.Ref {
		t.Fatalf("esperaba el pending_ref propuesto %v, obtuvo %v", p.Ref, ref)
	}
	fila, existe := filaPendiente(t, db, patHashed)
	if !existe {
		t.Fatal("esperaba la fila en pending_plates")
	}
	if fila.PendingRef != p.Ref {
		t.Fatalf("pending_ref persistido = %v, esperaba %v", fila.PendingRef, p.Ref)
	}
	if !fila.ExpiresAt.Truncate(time.Millisecond).Equal(p.ExpiresAt.Truncate(time.Millisecond)) {
		t.Fatalf("expires_at = %v, esperaba %v", fila.ExpiresAt, p.ExpiresAt)
	}
}

func TestFindOwnersOrReservePendingReutilizaLaReservaVigenteYExtiendeElVencimiento(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()

	primera := propuesta(ahora)
	if _, _, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, primera); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	creada, _ := filaPendiente(t, db, patHashed)

	segunda := propuesta(ahora.Add(3 * time.Hour))
	_, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, segunda)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if ref == nil || *ref != primera.Ref {
		t.Fatalf("debía reutilizar el pending_ref vigente %v, obtuvo %v", primera.Ref, ref)
	}
	fila, _ := filaPendiente(t, db, patHashed)
	if fila.PendingRef != primera.Ref {
		t.Fatalf("la fila debía conservar el pending_ref %v, tiene %v", primera.Ref, fila.PendingRef)
	}
	if !fila.ExpiresAt.Truncate(time.Millisecond).Equal(segunda.ExpiresAt.Truncate(time.Millisecond)) {
		t.Fatalf("expires_at = %v, esperaba extenderlo a %v", fila.ExpiresAt, segunda.ExpiresAt)
	}
	if !fila.CreatedAt.Equal(creada.CreatedAt) {
		t.Fatalf("created_at no debía cambiar al extender: %v != %v", fila.CreatedAt, creada.CreatedAt)
	}
}

func TestFindOwnersOrReservePendingNoReutilizaUnaReservaVencida(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	patHashed := uuid.NewString()
	ahora := time.Now().UTC()

	vieja := propuesta(ahora.Add(-2 * ventanaDePrueba))
	if _, _, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, vieja); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	nueva := propuesta(ahora)
	_, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, nueva)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if ref == nil || *ref != nueva.Ref {
		t.Fatalf("una reserva vencida no debía reutilizarse: esperaba %v, obtuvo %v", nueva.Ref, ref)
	}
	fila, _ := filaPendiente(t, db, patHashed)
	if fila.PendingRef != nueva.Ref {
		t.Fatalf("la fila debía tener el pending_ref nuevo %v, tiene %v", nueva.Ref, fila.PendingRef)
	}
	if !fila.CreatedAt.Truncate(time.Millisecond).Equal(nueva.Now.Truncate(time.Millisecond)) {
		t.Fatalf("created_at debía reiniciarse a %v, es %v", nueva.Now, fila.CreatedAt)
	}
}

// Dos resolve-owner simultáneos a la misma patente sin dueño deben terminar con
// un único pending_ref, aunque cada uno haya propuesto el suyo.
func TestFindOwnersOrReservePendingConcurrenteGeneraUnUnicoPendingRef(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db, WithQueryTimeout(10*time.Second))
	patHashed := uuid.NewString()
	const llamadores = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		refs = make(map[uuid.UUID]int)
		errs []error
	)
	inicio := make(chan struct{})
	for i := 0; i < llamadores; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-inicio
			_, ref, err := repo.FindOwnersOrReservePending(context.Background(), patHashed, propuesta(time.Now().UTC()))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			refs[*ref]++
		}()
	}
	close(inicio)
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("no se esperaban errores: %v", errs)
	}
	if len(refs) != 1 {
		t.Fatalf("esperaba un único pending_ref entre %d llamadas simultáneas, obtuvo %d: %v", llamadores, len(refs), refs)
	}
	var filas int64
	if err := db.Model(&pendingPlateModel{}).Where("plate_hmac = ?", patHashed).Count(&filas).Error; err != nil {
		t.Fatalf("no se pudo contar pending_plates: %v", err)
	}
	if filas != 1 {
		t.Fatalf("esperaba una única fila pendiente, obtuvo %d", filas)
	}
}

func TestFindOwnersOrReservePendingIndisponibilidadSeClasificaComoTal(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := repo.FindOwnersOrReservePending(ctx, uuid.NewString(), propuesta(time.Now().UTC()))

	if !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestDeleteExpiredPendingBorraSoloLasVencidas(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)
	ahora := time.Now().UTC()
	vencida, vigente := uuid.NewString(), uuid.NewString()

	if _, _, err := repo.FindOwnersOrReservePending(context.Background(), vencida, propuesta(ahora.Add(-2*ventanaDePrueba))); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if _, _, err := repo.FindOwnersOrReservePending(context.Background(), vigente, propuesta(ahora)); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	borradas, err := repo.DeleteExpiredPending(context.Background(), ahora)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if borradas != 1 {
		t.Fatalf("esperaba 1 fila borrada, obtuvo %d", borradas)
	}
	if _, existe := filaPendiente(t, db, vencida); existe {
		t.Fatal("la fila vencida debía borrarse")
	}
	if _, existe := filaPendiente(t, db, vigente); !existe {
		t.Fatal("la fila vigente no debía borrarse")
	}
}
