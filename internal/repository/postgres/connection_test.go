package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func TestResetSchemaEliminaTablasYDatosExistentes(t *testing.T) {
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
		t.Fatalf("no se pudo crear el vehículo de prueba: %v", err)
	}

	if err := ResetSchema(db); err != nil {
		t.Fatalf("no se pudo reiniciar el esquema: %v", err)
	}

	if err := db.Exec("SELECT 1 FROM vehicles").Error; err == nil {
		t.Fatal("esperaba que la tabla vehicles ya no existiera tras ResetSchema")
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("no se pudo migrar el esquema tras el reinicio: %v", err)
	}

	var total int64
	if err := db.Table("vehicles").Count(&total).Error; err != nil {
		t.Fatalf("no se pudo contar filas tras la remigración: %v", err)
	}
	if total != 0 {
		t.Fatalf("esperaba la tabla vacía tras ResetSchema + AutoMigrate, tiene %d filas", total)
	}
}
