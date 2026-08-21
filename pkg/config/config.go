// Package config centraliza la carga de configuración del servicio desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config agrupa los parámetros de configuración requeridos por el servicio.
type Config struct {
	Port                  string
	DatabaseURL           string
	VehicleHPKEPrivateKey string
	PatAESKey             string
	PatHMACSecret         string
	IdentityInternalURL   string
	IdentityTimeoutMS     int
}

// Load construye la configuración a partir de las variables de entorno del proceso.
// Retorna error si falta alguna variable obligatoria o si su formato es inválido.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                  os.Getenv("PORT"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		VehicleHPKEPrivateKey: os.Getenv("VEHICLE_HPKE_PRIVATE_KEY"),
		PatAESKey:             os.Getenv("PAT_AES_KEY"),
		PatHMACSecret:         os.Getenv("PAT_HMAC_SECRET"),
		IdentityInternalURL:   os.Getenv("IDENTITY_INTERNAL_URL"),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
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

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL es obligatoria")
	}
	if cfg.IdentityInternalURL == "" {
		return nil, fmt.Errorf("config: IDENTITY_INTERNAL_URL es obligatoria")
	}

	return cfg, nil
}
