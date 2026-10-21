// Package postgres implementa la persistencia de vehículos sobre PostgreSQL vía GORM.
package postgres

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	// connectTimeoutSeconds acota la fase de dial contra PostgreSQL: sin este
	// límite, un destino que nunca responde al handshake TCP retiene la conexión
	// indefinidamente en lugar de fallar rápido.
	connectTimeoutSeconds = 5

	// Límites del pool de conexiones subyacente (*sql.DB). Sin ellos, una ráfaga de
	// solicitudes concurrentes abre conexiones sin techo hacia la base de datos.
	maxOpenConns    = 10
	maxIdleConns    = 5
	connMaxLifetime = 30 * time.Minute
	connMaxIdleTime = 5 * time.Minute
)

// Connect abre la conexión a PostgreSQL a partir de la cadena de conexión
// configurada. Aplica un connect_timeout explícito (si la cadena no fijó uno
// propio) y los límites del pool de conexiones subyacente.
func Connect(dsn string) (*gorm.DB, error) {
	dsnConTimeout, err := withConnectTimeout(dsn, connectTimeoutSeconds)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(gormpostgres.Open(dsnConTimeout), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("postgres: no se pudo conectar a la base de datos: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres: no se pudo obtener la conexión subyacente: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)

	return db, nil
}

// withConnectTimeout agrega el parámetro connect_timeout (en segundos) a dsn
// si la cadena no especificó uno propio, preservando el resto de los
// parámetros existentes. Respeta la elección explícita del operador cuando
// connect_timeout ya está presente.
func withConnectTimeout(dsn string, seconds int) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("postgres: no se pudo interpretar la cadena de conexión: %w", err)
	}

	query := parsed.Query()
	if query.Get("connect_timeout") == "" {
		query.Set("connect_timeout", strconv.Itoa(seconds))
		parsed.RawQuery = query.Encode()
	}

	return parsed.String(), nil
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

// AutoMigrate crea o actualiza el esquema de las tablas vehicles y pending_plates.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&vehicleModel{}, &pendingPlateModel{}); err != nil {
		return fmt.Errorf("postgres: no se pudo migrar el esquema: %w", err)
	}
	return nil
}
