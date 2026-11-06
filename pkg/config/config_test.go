package config

import (
	"strings"
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
		"IA_HPKE_PUBLIC_KEY":       "clave-de-prueba-ia",
		"AUDIT_PUBLIC_KEY":         "clave-de-prueba-audit",
		"AUDIT_KEY_ID":             "2026-11",
		"MOCK_DNRPA_OWNER_DNI":     "12345678",
		"INTERNAL_SERVICE_TOKEN":   "token-de-prueba",
		"RABBITMQ_URL":             "amqp://guest:guest@localhost:5672/",
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
	if cfg.PendingDeliveryTTL != 24*time.Hour {
		t.Errorf("esperaba PENDING_DELIVERY_HOURS por defecto de 24h, obtuvo %v", cfg.PendingDeliveryTTL)
	}
	if cfg.SweeperInterval != time.Minute {
		t.Errorf("esperaba SWEEPER_INTERVAL_MS por defecto de 60s, obtuvo %v", cfg.SweeperInterval)
	}
	if cfg.IAHPKEPublicKey != "clave-de-prueba-ia" {
		t.Errorf("esperaba IA_HPKE_PUBLIC_KEY cargada, obtuvo %q", cfg.IAHPKEPublicKey)
	}
	if cfg.AuditPublicKey != "clave-de-prueba-audit" {
		t.Errorf("esperaba AUDIT_PUBLIC_KEY cargada, obtuvo %q", cfg.AuditPublicKey)
	}
	if cfg.AuditKeyID != "2026-11" {
		t.Errorf("esperaba AUDIT_KEY_ID cargada, obtuvo %q", cfg.AuditKeyID)
	}
}

func TestLoadAuditKeyIDMasLargoQueLaColumnaDeAuditFalla(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("AUDIT_KEY_ID", strings.Repeat("k", 33))

	if _, err := Load(); err == nil {
		t.Fatal("esperaba error con un AUDIT_KEY_ID de más de 32 caracteres")
	}

	t.Setenv("AUDIT_KEY_ID", strings.Repeat("k", 32))
	if _, err := Load(); err != nil {
		t.Fatalf("un AUDIT_KEY_ID de 32 caracteres debía aceptarse: %v", err)
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
		"IA_HPKE_PUBLIC_KEY",
		"AUDIT_PUBLIC_KEY",
		"AUDIT_KEY_ID",
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

func TestLoadPendingDeliveryHoursConfigurable(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("PENDING_DELIVERY_HOURS", "2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.PendingDeliveryTTL != 2*time.Hour {
		t.Fatalf("esperaba 2h, obtuvo %v", cfg.PendingDeliveryTTL)
	}
}

func TestLoadPendingDeliveryHoursInvalidaFalla(t *testing.T) {
	for _, valor := range []string{"abc", "0", "-3", "1.5"} {
		t.Run(valor, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv("PENDING_DELIVERY_HOURS", valor)

			if _, err := Load(); err == nil {
				t.Fatalf("PENDING_DELIVERY_HOURS=%q debía fallar el arranque", valor)
			}
		})
	}
}

func TestLoadSweeperIntervalInvalidoFalla(t *testing.T) {
	for _, valor := range []string{"abc", "0"} {
		t.Run(valor, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv("SWEEPER_INTERVAL_MS", valor)

			if _, err := Load(); err == nil {
				t.Fatalf("SWEEPER_INTERVAL_MS=%q debía fallar el arranque", valor)
			}
		})
	}
}

func TestLoadRabbitMQPorDefecto(t *testing.T) {
	setEnvCompleto(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.RabbitMQURL != "amqp://guest:guest@localhost:5672/" {
		t.Errorf("RABBITMQ_URL no cargada: %q", cfg.RabbitMQURL)
	}
	if cfg.RabbitMQExchange != "vvi.topic.exchange" {
		t.Errorf("esperaba exchange por defecto vvi.topic.exchange, obtuvo %q", cfg.RabbitMQExchange)
	}
	if cfg.RabbitMQPublishTimeout != 2*time.Second {
		t.Errorf("esperaba timeout de publicación por defecto de 2s, obtuvo %v", cfg.RabbitMQPublishTimeout)
	}
	if cfg.OutboxWorkerInterval != 2*time.Second {
		t.Errorf("esperaba intervalo del worker por defecto de 2s, obtuvo %v", cfg.OutboxWorkerInterval)
	}
	if cfg.OutboxWorkerBatchSize != 20 {
		t.Errorf("esperaba lote por defecto de 20, obtuvo %d", cfg.OutboxWorkerBatchSize)
	}
}

func TestLoadSinRabbitMQURLFalla(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("RABBITMQ_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("RABBITMQ_URL es obligatoria: el arranque debía fallar")
	}
}

func TestLoadParametrosDeRabbitMQInvalidosFallan(t *testing.T) {
	casos := map[string]string{
		"RABBITMQ_PUBLISH_TIMEOUT_MS": "0",
		"OUTBOX_WORKER_INTERVAL_MS":   "abc",
		"OUTBOX_WORKER_BATCH_SIZE":    "0",
	}
	for nombre, valor := range casos {
		t.Run(nombre, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv(nombre, valor)

			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q debía fallar el arranque", nombre, valor)
			}
		})
	}
}

func TestLoadConsumidorUserDeletedConValoresPorDefecto(t *testing.T) {
	setEnvCompleto(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.UserDeletedQueue != "vehicle.user-deleted.q" {
		t.Errorf("esperaba la cola por defecto vehicle.user-deleted.q, obtuvo %q", cfg.UserDeletedQueue)
	}
	if cfg.RabbitMQPrefetch != 10 {
		t.Errorf("esperaba RABBITMQ_PREFETCH por defecto 10, obtuvo %d", cfg.RabbitMQPrefetch)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("esperaba MAX_RETRIES por defecto 5, obtuvo %d", cfg.MaxRetries)
	}
}

func TestLoadConsumidorUserDeletedConfigurablePorEnv(t *testing.T) {
	setEnvCompleto(t)
	t.Setenv("USER_DELETED_QUEUE", "vehicle.otra.q")
	t.Setenv("RABBITMQ_PREFETCH", "3")
	t.Setenv("MAX_RETRIES", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if cfg.UserDeletedQueue != "vehicle.otra.q" || cfg.RabbitMQPrefetch != 3 || cfg.MaxRetries != 0 {
		t.Errorf("cola=%q prefetch=%d reintentos=%d", cfg.UserDeletedQueue, cfg.RabbitMQPrefetch, cfg.MaxRetries)
	}
}

func TestLoadPrefetchYMaxRetriesInvalidos(t *testing.T) {
	invalidos := []struct{ nombre, valor string }{
		{"RABBITMQ_PREFETCH", "no-es-un-numero"},
		{"RABBITMQ_PREFETCH", "0"},
		{"RABBITMQ_PREFETCH", "-1"},
		{"RABBITMQ_PREFETCH", "65536"},
		{"MAX_RETRIES", "no-es-un-numero"},
		{"MAX_RETRIES", "-1"},
	}
	for _, caso := range invalidos {
		t.Run(caso.nombre+"="+caso.valor, func(t *testing.T) {
			setEnvCompleto(t)
			t.Setenv(caso.nombre, caso.valor)

			if _, err := Load(); err == nil {
				t.Fatalf("esperaba error con %s=%s", caso.nombre, caso.valor)
			}
		})
	}
}
