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
		db.Exec("DELETE FROM pending_plates")
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

func TestExistsByPatHashPatenteExistenteRetornaTrue(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	patHashed := uuid.NewString()
	vehicle := &domain.Vehicle{ID: uuid.New(), OwnerID: uuid.New(), PatHashed: patHashed, PatAES: "cifrado", CreatedAt: time.Now()}
	if err := repo.Create(context.Background(), vehicle); err != nil {
		t.Fatalf("no se pudo crear el vehículo: %v", err)
	}

	exists, err := repo.ExistsByPatHash(context.Background(), patHashed)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !exists {
		t.Fatal("esperaba true para una patente vinculada")
	}
}

func TestExistsByPatHashPatenteInexistenteRetornaFalse(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVehicleRepository(db)

	exists, err := repo.ExistsByPatHash(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if exists {
		t.Fatal("esperaba false para una patente no vinculada")
	}
}
