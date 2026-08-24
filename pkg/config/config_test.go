package config

import "testing"

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
