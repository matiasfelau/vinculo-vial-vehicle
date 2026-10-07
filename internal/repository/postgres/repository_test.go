package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// setupTestDB requiere una instancia de PostgreSQL alcanzable vía DATABASE_URL.
// Si la variable no está configurada, la prueba se omite: no se simula PostgreSQL
// con otro motor porque el mapeo de errores depende del SQLSTATE real de pgx.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL no configurada: se omite prueba de integración con PostgreSQL")
	}

	db, err := Connect(dsn)
	if err != nil {
		t.Fatalf("no se pudo conectar a la base de datos de prueba: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("no se pudo migrar el esquema: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM vehicles")
	})

	return db
}

func TestCreatePersisteVehiculo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	vehicle := &domain.Vehicle{
		ID:        uuid.New(),
		OwnerID:   uuid.New(),
		PatHashed: uuid.NewString(),
		PatAES:    "cifrado-de-prueba",
		CreatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), vehicle); err != nil {
		t.Fatalf("no se pudo crear el vehículo: %v", err)
	}
}

func TestCreateMismoPropietarioMismaPatenteRetornaDuplicado(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	ownerID := uuid.New()
	patHashed := uuid.NewString()

	primero := &domain.Vehicle{ID: uuid.New(), OwnerID: ownerID, PatHashed: patHashed, PatAES: "cifrado-1", CreatedAt: time.Now()}
	if err := repo.Create(context.Background(), primero); err != nil {
		t.Fatalf("no se pudo crear el primer vehículo: %v", err)
	}

	segundo := &domain.Vehicle{ID: uuid.New(), OwnerID: ownerID, PatHashed: patHashed, PatAES: "cifrado-2", CreatedAt: time.Now()}
	err := repo.Create(context.Background(), segundo)
	if !errors.Is(err, domain.ErrDuplicateVehicle) {
		t.Fatalf("esperaba domain.ErrDuplicateVehicle, obtuvo %v", err)
	}
}

func TestCreateDistintoPropietarioMismaPatenteSePermite(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	patHashed := uuid.NewString()

	primero := &domain.Vehicle{ID: uuid.New(), OwnerID: uuid.New(), PatHashed: patHashed, PatAES: "cifrado-1", CreatedAt: time.Now()}
	if err := repo.Create(context.Background(), primero); err != nil {
		t.Fatalf("no se pudo crear el primer vehículo: %v", err)
	}

	segundo := &domain.Vehicle{ID: uuid.New(), OwnerID: uuid.New(), PatHashed: patHashed, PatAES: "cifrado-2", CreatedAt: time.Now()}
	if err := repo.Create(context.Background(), segundo); err != nil {
		t.Fatalf("esperaba poder vincular la misma patente a otro propietario, obtuvo error: %v", err)
	}
}

func TestFindOwnersByPatHashRetornaLosTresDuenos(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	patHashed := uuid.NewString()
	esperados := make(map[uuid.UUID]bool)
	for i := 0; i < 3; i++ {
		ownerID := uuid.New()
		esperados[ownerID] = true

		vehicle := &domain.Vehicle{ID: uuid.New(), OwnerID: ownerID, PatHashed: patHashed, PatAES: "cifrado", CreatedAt: time.Now()}
		if err := repo.Create(context.Background(), vehicle); err != nil {
			t.Fatalf("no se pudo crear el vehículo %d: %v", i, err)
		}
	}

	owners, err := repo.FindOwnersByPatHash(context.Background(), patHashed)
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
}

func TestFindOwnersByPatHashPatenteInexistenteRetornaVacioSinError(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	owners, err := repo.FindOwnersByPatHash(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(owners) != 0 {
		t.Fatalf("esperaba un slice vacío, obtuvo %d elementos", len(owners))
	}
}
