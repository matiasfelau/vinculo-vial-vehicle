// Package postgres implementa la persistencia de vehículos sobre PostgreSQL vía GORM.
package postgres

import (
	"fmt"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Connect abre la conexión a PostgreSQL a partir de la cadena de conexión configurada.
func Connect(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("postgres: no se pudo conectar a la base de datos: %w", err)
	}
	return db, nil
}

// ResetSchema elimina y recrea el esquema public completo, destruyendo todos los
// datos existentes. Uso exclusivo de despliegues efímeros (EPHEMERAL_DB=true) para
// garantizar un estado limpio en pruebas end-to-end; nunca debe ejecutarse contra
// una base con datos reales.
func ResetSchema(db *gorm.DB) error {
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		return fmt.Errorf("postgres: no se pudo reiniciar el esquema: %w", err)
	}
	return nil
}

// AutoMigrate crea o actualiza el esquema de la tabla vehicles.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&vehicleModel{}); err != nil {
		return fmt.Errorf("postgres: no se pudo migrar el esquema: %w", err)
	}
	return nil
}
