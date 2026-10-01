package config

import (
	"testing"
	"time"
)

func setEnvCompleto(t *testing.T) {
	t.Helper()

	vars := map[string]string{
		"DATABASE_URL":             "postgres://vehicle:vehicle@localhost:5432/vehicle?sslmode=disable",
		"IDENTITY_INTERNAL_URL":    "http://identity:8080/internal/verify-ownership",
		"VEHICLE_HPKE_PRIVATE_KEY": "clave-de-prueba",
		"PAT_AES_KEY":              "clave-de-prueba",
		"PAT_HMAC_SECRET":          "clave-de-prueba",
		"IDENTITY_HPKE_PUBLIC_KEY": "clave-de-prueba",
		"MOCK_DNRPA_OWNER_DNI":     "12345678",
		"INTERNAL_SERVICE_TOKEN":   "token-de-prueba",
	}

	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoadConConfiguracionCompleta(t *testing.T) {
	setEnvCompleto(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("esperaba puerto por defecto 8080, obtuvo %q", cfg.Port)
	}
	if cfg.IdentityTimeoutMS != 2000 {
		t.Errorf("esperaba timeout por defecto 2000, obtuvo %d", cfg.IdentityTimeoutMS)
	}
	if cfg.EphemeralDB != false {
		t.Errorf("esperaba EPHEMERAL_DB por defecto false, obtuvo %v", cfg.EphemeralDB)
	}
	if cfg.InternalPort != "8081" {
		t.Errorf("esperaba puerto interno por defecto 8081, obtuvo %q", cfg.InternalPort)
	}
	if cfg.IdentityCBFailureThreshold != 5 {
		t.Errorf("esperaba umbral de fallas por defecto 5, obtuvo %d", cfg.IdentityCBFailureThreshold)
	}
	if cfg.IdentityCBWindow != 30*time.Second {
		t.Errorf("esperaba ventana por defecto de 30s, obtuvo %v", cfg.IdentityCBWindow)
	}
	if cfg.IdentityCBOpenDuration != 30*time.Second {
		t.Errorf("esperaba tiempo de apertura por defecto de 30s, obtuvo %v", cfg.IdentityCBOpenDuration)
	}
	if cfg.DBQueryTimeout != 1500*time.Millisecond {
		t.Errorf("esperaba timeout de consulta por defecto de 1500ms, obtuvo %v", cfg.DBQueryTimeout)
	}
}

func TestLoadDBQueryTimeoutConfigurablePorEnv(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("DB_QUERY_TIMEOUT_MS", "1500")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.DBQueryTimeout != 1500*time.Millisecond {
		t.Errorf("esperaba timeout de consulta de 1500ms, obtuvo %v", cfg.DBQueryTimeout)
	}
}

func TestLoadDBQueryTimeoutInvalido(t *testing.T) {
	casos := []string{"no-es-un-numero", "0", "-100"}

	for _, valor := range casos {
		t.Run(valor, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv("DB_QUERY_TIMEOUT_MS", valor)

			if _, err := Load(); err == nil {
				t.Fatalf("esperaba error con DB_QUERY_TIMEOUT_MS=%q", valor)
			}
		})
	}
}

func TestLoadCircuitBreakerConfigurablePorEnv(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("IDENTITY_CB_FAILURE_THRESHOLD", "3")
	t.Setenv("IDENTITY_CB_WINDOW_MS", "10000")
	t.Setenv("IDENTITY_CB_OPEN_DURATION_MS", "5000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.IdentityCBFailureThreshold != 3 {
		t.Errorf("esperaba umbral 3, obtuvo %d", cfg.IdentityCBFailureThreshold)
	}
	if cfg.IdentityCBWindow != 10*time.Second {
		t.Errorf("esperaba ventana de 10s, obtuvo %v", cfg.IdentityCBWindow)
	}
	if cfg.IdentityCBOpenDuration != 5*time.Second {
		t.Errorf("esperaba tiempo de apertura de 5s, obtuvo %v", cfg.IdentityCBOpenDuration)
	}
}

func TestLoadCircuitBreakerConfigInvalida(t *testing.T) {
	casos := []struct {
		nombre string
		clave  string
		valor  string
	}{
		{"umbral no numérico", "IDENTITY_CB_FAILURE_THRESHOLD", "no-es-un-numero"},
		{"ventana no numérica", "IDENTITY_CB_WINDOW_MS", "no-es-un-numero"},
		{"ventana negativa", "IDENTITY_CB_WINDOW_MS", "-1"},
		{"apertura cero", "IDENTITY_CB_OPEN_DURATION_MS", "0"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv(c.clave, c.valor)

			if _, err := Load(); err == nil {
				t.Fatalf("esperaba error con %s=%q", c.clave, c.valor)
			}
		})
	}
}

func TestLoadInternalPortIgualAPortRetornaError(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("PORT", "9000")
	t.Setenv("INTERNAL_PORT", "9000")

	if _, err := Load(); err == nil {
		t.Fatal("esperaba error cuando INTERNAL_PORT coincide con PORT")
	}
}

func TestLoadEphemeralDBTrue(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("EPHEMERAL_DB", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !cfg.EphemeralDB {
		t.Error("esperaba EphemeralDB en true")
	}
}

func TestLoadEphemeralDBInvalido(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("EPHEMERAL_DB", "no-es-un-booleano")

	if _, err := Load(); err == nil {
		t.Fatal("esperaba error con EPHEMERAL_DB inválido")
	}
}

func TestLoadFallaSinVariablesObligatorias(t *testing.T) {
	obligatorias := []string{
		"DATABASE_URL",
		"IDENTITY_INTERNAL_URL",
		"VEHICLE_HPKE_PRIVATE_KEY",
		"PAT_AES_KEY",
		"PAT_HMAC_SECRET",
		"IDENTITY_HPKE_PUBLIC_KEY",
		"MOCK_DNRPA_OWNER_DNI",
		"INTERNAL_SERVICE_TOKEN",
	}

	for _, faltante := range obligatorias {
		t.Run(faltante, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv(faltante, "")

			if _, err := Load(); err == nil {
				t.Fatalf("esperaba error al faltar %s", faltante)
			}
		})
	}
}
