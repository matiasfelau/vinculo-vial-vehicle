// Package config centraliza la carga de configuración del servicio desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// defaultPendingDeliveryHours es la ventana de entrega de alertas pendientes
// (US-10) cuando PENDING_DELIVERY_HOURS no está definida.
const defaultPendingDeliveryHours = "24"

// defaultRabbitMQExchange es el exchange topic de la plataforma.
const defaultRabbitMQExchange = "vvi.topic.exchange"

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
	IAHPKEPublicKey       string
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

	// PendingDeliveryTTL es PENDING_DELIVERY_HOURS (US-10): cuánto conserva Vehicle
	// la reserva de una patente sin dueños, contado desde la última alerta.
	PendingDeliveryTTL time.Duration

	// SweeperInterval es cada cuánto corre el sweeper de patentes pendientes vencidas.
	SweeperInterval time.Duration

	// RabbitMQURL y RabbitMQExchange identifican el broker y el exchange topic donde
	// se publican los eventos. El exchange lo declara vinculo-vial-core.
	RabbitMQURL      string
	RabbitMQExchange string

	// RabbitMQPublishTimeout acota cada publicación, incluida la espera del confirm.
	RabbitMQPublishTimeout time.Duration

	// OutboxWorkerInterval y OutboxWorkerBatchSize gobiernan el worker que publica
	// el outbox: cada cuánto revisa los pendientes y cuántos intenta por ciclo.
	OutboxWorkerInterval  time.Duration
	OutboxWorkerBatchSize int
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
		IAHPKEPublicKey:       os.Getenv("IA_HPKE_PUBLIC_KEY"),
		MockDNRPAOwnerDNI:     os.Getenv("MOCK_DNRPA_OWNER_DNI"),
		InternalServiceToken:  os.Getenv("INTERNAL_SERVICE_TOKEN"),
		RabbitMQURL:           os.Getenv("RABBITMQ_URL"),
		RabbitMQExchange:      os.Getenv("RABBITMQ_EXCHANGE"),
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

	pendingHoursRaw := os.Getenv("PENDING_DELIVERY_HOURS")
	if pendingHoursRaw == "" {
		pendingHoursRaw = defaultPendingDeliveryHours
	}
	pendingHours, err := strconv.Atoi(pendingHoursRaw)
	if err != nil {
		return nil, fmt.Errorf("config: PENDING_DELIVERY_HOURS inválido: %w", err)
	}
	if pendingHours <= 0 {
		return nil, fmt.Errorf("config: PENDING_DELIVERY_HOURS debe ser mayor a cero")
	}
	cfg.PendingDeliveryTTL = time.Duration(pendingHours) * time.Hour

	sweeperInterval, err := parsePositiveMillis("SWEEPER_INTERVAL_MS", "60000")
	if err != nil {
		return nil, err
	}
	cfg.SweeperInterval = sweeperInterval

	if cfg.RabbitMQExchange == "" {
		cfg.RabbitMQExchange = defaultRabbitMQExchange
	}

	publishTimeout, err := parsePositiveMillis("RABBITMQ_PUBLISH_TIMEOUT_MS", "2000")
	if err != nil {
		return nil, err
	}
	cfg.RabbitMQPublishTimeout = publishTimeout

	outboxInterval, err := parsePositiveMillis("OUTBOX_WORKER_INTERVAL_MS", "2000")
	if err != nil {
		return nil, err
	}
	cfg.OutboxWorkerInterval = outboxInterval

	batchSizeRaw := os.Getenv("OUTBOX_WORKER_BATCH_SIZE")
	if batchSizeRaw == "" {
		batchSizeRaw = "20"
	}
	batchSize, err := strconv.Atoi(batchSizeRaw)
	if err != nil {
		return nil, fmt.Errorf("config: OUTBOX_WORKER_BATCH_SIZE inválido: %w", err)
	}
	if batchSize < 1 {
		return nil, fmt.Errorf("config: OUTBOX_WORKER_BATCH_SIZE debe ser mayor o igual a 1")
	}
	cfg.OutboxWorkerBatchSize = batchSize

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
	if cfg.IAHPKEPublicKey == "" {
		return nil, fmt.Errorf("config: IA_HPKE_PUBLIC_KEY es obligatoria")
	}
	if cfg.RabbitMQURL == "" {
		return nil, fmt.Errorf("config: RABBITMQ_URL es obligatoria")
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
