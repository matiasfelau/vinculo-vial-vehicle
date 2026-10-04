// Package config centraliza la carga de configuración del servicio desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config agrupa los parámetros de configuración requeridos por el servicio.
type Config struct {
	Port                  string
	InternalPort          string
	DatabaseURL           string
	VehicleHPKEPrivateKey string
	PatAESKey             string
	PatHMACSecret         string
	IdentityInternalURL   string
	IdentityTimeoutMS     int
	IdentityHPKEPublicKey string
	MockDNRPAOwnerDNI     string
	InternalServiceToken  string
	EphemeralDB           bool

	// Parámetros del circuit breaker hacia identity (ver internal/breaker).
	IdentityCBFailureThreshold int
	IdentityCBWindow           time.Duration
	IdentityCBOpenDuration     time.Duration

	// DBQueryTimeout acota cada consulta contra la base de datos propia (ver
	// internal/repository/postgres.WithQueryTimeout).
	DBQueryTimeout time.Duration
}

// Load construye la configuración a partir de las variables de entorno del proceso.
// Retorna error si falta alguna variable obligatoria o si su formato es inválido.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                  os.Getenv("PORT"),
		InternalPort:          os.Getenv("INTERNAL_PORT"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		VehicleHPKEPrivateKey: os.Getenv("VEHICLE_HPKE_PRIVATE_KEY"),
		PatAESKey:             os.Getenv("PAT_AES_KEY"),
		PatHMACSecret:         os.Getenv("PAT_HMAC_SECRET"),
		IdentityInternalURL:   os.Getenv("IDENTITY_INTERNAL_URL"),
		IdentityHPKEPublicKey: os.Getenv("IDENTITY_HPKE_PUBLIC_KEY"),
		MockDNRPAOwnerDNI:     os.Getenv("MOCK_DNRPA_OWNER_DNI"),
		InternalServiceToken:  os.Getenv("INTERNAL_SERVICE_TOKEN"),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.InternalPort == "" {
		cfg.InternalPort = "8081"
	}
	if cfg.InternalPort == cfg.Port {
		return nil, fmt.Errorf("config: INTERNAL_PORT no puede coincidir con PORT: el router interno perdería su puerto propio")
	}

	timeoutRaw := os.Getenv("IDENTITY_TIMEOUT_MS")
	if timeoutRaw == "" {
		timeoutRaw = "2000"
	}
	timeoutMS, err := strconv.Atoi(timeoutRaw)
	if err != nil {
		return nil, fmt.Errorf("config: IDENTITY_TIMEOUT_MS inválido: %w", err)
	}
	cfg.IdentityTimeoutMS = timeoutMS

	ephemeralDBRaw := os.Getenv("EPHEMERAL_DB")
	if ephemeralDBRaw == "" {
		ephemeralDBRaw = "false"
	}
	ephemeralDB, err := strconv.ParseBool(ephemeralDBRaw)
	if err != nil {
		return nil, fmt.Errorf("config: EPHEMERAL_DB inválido: %w", err)
	}
	cfg.EphemeralDB = ephemeralDB

	cbThresholdRaw := os.Getenv("IDENTITY_CB_FAILURE_THRESHOLD")
	if cbThresholdRaw == "" {
		cbThresholdRaw = "5"
	}
	cbThreshold, err := strconv.Atoi(cbThresholdRaw)
	if err != nil {
		return nil, fmt.Errorf("config: IDENTITY_CB_FAILURE_THRESHOLD inválido: %w", err)
	}
	cfg.IdentityCBFailureThreshold = cbThreshold

	cbWindowMS, err := parsePositiveMillis("IDENTITY_CB_WINDOW_MS", "30000")
	if err != nil {
		return nil, err
	}
	cfg.IdentityCBWindow = cbWindowMS

	cbOpenDurationMS, err := parsePositiveMillis("IDENTITY_CB_OPEN_DURATION_MS", "30000")
	if err != nil {
		return nil, err
	}
	cfg.IdentityCBOpenDuration = cbOpenDurationMS

	// El default debe mantenerse por debajo del timeout servicio-a-servicio con el
	// que vinculo-vial-alert llama a este servicio (2000ms): si la consulta a la
	// base propia pudiera tardar más que eso, Alert abandonaría la espera antes de
	// que Vehicle llegue a responder el 503, violando la regla de anidamiento de
	// timeouts de la plataforma (servicio→servicio > deadline de consulta DB).
	dbQueryTimeoutMS, err := parsePositiveMillis("DB_QUERY_TIMEOUT_MS", "1500")
	if err != nil {
		return nil, err
	}
	cfg.DBQueryTimeout = dbQueryTimeoutMS

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL es obligatoria")
	}
	if cfg.IdentityInternalURL == "" {
		return nil, fmt.Errorf("config: IDENTITY_INTERNAL_URL es obligatoria")
	}
	if cfg.VehicleHPKEPrivateKey == "" {
		return nil, fmt.Errorf("config: VEHICLE_HPKE_PRIVATE_KEY es obligatoria")
	}
	if cfg.PatAESKey == "" {
		return nil, fmt.Errorf("config: PAT_AES_KEY es obligatoria")
	}
	if cfg.PatHMACSecret == "" {
		return nil, fmt.Errorf("config: PAT_HMAC_SECRET es obligatoria")
	}
	if cfg.IdentityHPKEPublicKey == "" {
		return nil, fmt.Errorf("config: IDENTITY_HPKE_PUBLIC_KEY es obligatoria")
	}
	if cfg.MockDNRPAOwnerDNI == "" {
		return nil, fmt.Errorf("config: MOCK_DNRPA_OWNER_DNI es obligatoria")
	}
	if cfg.InternalServiceToken == "" {
		return nil, fmt.Errorf("config: INTERNAL_SERVICE_TOKEN es obligatoria")
	}

	return cfg, nil
}

// parsePositiveMillis lee la variable de entorno name como una cantidad de
// milisegundos y la convierte a time.Duration, usando defaultRaw cuando la
// variable no está definida.
func parsePositiveMillis(name, defaultRaw string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		raw = defaultRaw
	}

	ms, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s inválido: %w", name, err)
	}
	if ms <= 0 {
		return 0, fmt.Errorf("config: %s debe ser mayor a cero", name)
	}

	return time.Duration(ms) * time.Millisecond, nil
}
