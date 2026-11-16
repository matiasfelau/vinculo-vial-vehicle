package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/matiasfelau/vinculo-vial-vehicle/pkg/config"
)

// brokerInalcanzable es una URL AMQP a un puerto donde no escucha nadie: el
// servicio debe arrancar igual, en modo degradado.
const brokerInalcanzable = "amqp://guest:guest@127.0.0.1:1/"

// puertoLibre devuelve un puerto TCP que nadie usa en este momento.
func puertoLibre(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no se pudo reservar un puerto: %v", err)
	}
	defer l.Close()

	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("dirección inesperada %q: %v", l.Addr(), err)
	}
	return port
}

// configValida arma una configuración completa y válida para run, sin base de
// datos real: DatabaseURL queda para cada prueba. Los puertos son libres y los
// intervalos son cortos para que el apagado sea rápido.
func configValida(t *testing.T) *config.Config {
	t.Helper()

	iaPublic, _ := newKeyPair(t)
	auditPublic, _ := newKeyPair(t)
	_, vehiclePrivate := newKeyPair(t)
	rawPrivate, err := vehiclePrivate.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}

	return &config.Config{
		Port:                   puertoLibre(t),
		InternalPort:           puertoLibre(t),
		VehicleHPKEPrivateKey:  base64.StdEncoding.EncodeToString(rawPrivate),
		PatAESKey:              base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
		PatHMACSecret:          "secreto-de-prueba",
		IAHPKEPublicKey:        iaPublic,
		AuditPublicKey:         auditPublic,
		AuditKeyID:             "audit-test",
		InternalServiceToken:   "token-interno-de-prueba",
		VehicleMaxPerUser:      3,
		DBQueryTimeout:         2 * time.Second,
		PendingDeliveryTTL:     time.Hour,
		SweeperInterval:        50 * time.Millisecond,
		RabbitMQURL:            brokerInalcanzable,
		RabbitMQExchange:       "vvi.test.run",
		RabbitMQPublishTimeout: time.Second,
		OutboxWorkerInterval:   50 * time.Millisecond,
		OutboxWorkerBatchSize:  10,
		UserDeletedQueue:       "vehicle.run-test.q",
		RabbitMQPrefetch:       1,
		MaxRetries:             3,
	}
}

// baseAislada crea una base PostgreSQL nueva y vacía a partir de DATABASE_URL y
// devuelve su DSN. run migra (y con EPHEMERAL_DB borra) el esquema entero: usar la
// base compartida pisaría a las pruebas de otros paquetes que corren en paralelo.
func baseAislada(t *testing.T) string {
	t.Helper()

	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Skip("DATABASE_URL no configurada: se omite la prueba contra PostgreSQL")
	}
	dsn, err := url.Parse(base)
	if err != nil {
		t.Fatalf("DATABASE_URL inválida: %v", err)
	}

	admin, err := gorm.Open(postgres.Open(base), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("no se pudo conectar a PostgreSQL: %v", err)
	}
	nombre := "vehicle_run_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := admin.Exec("CREATE DATABASE " + nombre).Error; err != nil {
		t.Fatalf("no se pudo crear la base aislada: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE IF EXISTS " + nombre + " WITH (FORCE)")
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	dsn.Path = "/" + nombre
	return dsn.String()
}

// dsnSinPermisos devuelve el DSN de un rol que puede conectarse a la base aislada
// pero no es dueño de su esquema public ni puede crear objetos en él: migrar o
// reiniciar el esquema con ese rol tiene que fallar.
func dsnSinPermisos(t *testing.T, dsnAislado string) string {
	t.Helper()

	dsn, err := url.Parse(dsnAislado)
	if err != nil {
		t.Fatalf("DSN inválido: %v", err)
	}
	admin, err := gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("no se pudo conectar a PostgreSQL: %v", err)
	}
	rol := "vv_sin_permisos_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := admin.Exec("CREATE ROLE " + rol + " LOGIN PASSWORD 'sin-permisos'").Error; err != nil {
		t.Fatalf("no se pudo crear el rol de prueba: %v", err)
	}
	t.Cleanup(func() {
		// Corre antes que el DROP DATABASE de baseAislada (cleanups LIFO): el rol
		// no es dueño de nada, así que se puede borrar aunque la base siga viva.
		admin.Exec("DROP ROLE IF EXISTS " + rol)
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	dsn.User = url.UserPassword(rol, "sin-permisos")
	return dsn.String()
}

// resultadoRun recoge cómo terminó run.
type resultadoRun struct {
	err error
}

// lanzarRun ejecuta run en una goroutine y devuelve cómo cancelarlo y el canal
// donde llega su resultado.
func lanzarRun(t *testing.T, cfg *config.Config) (cancel context.CancelFunc, resultado <-chan resultadoRun) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan resultadoRun, 1)
	terminado := make(chan struct{})
	go func() {
		defer close(terminado)
		ch <- resultadoRun{err: run(ctx, cfg)}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-terminado:
		case <-time.After(20 * time.Second):
		}
	})
	return cancel, ch
}

// esperarHTTP consulta endpoint hasta que responda con el status pedido.
func esperarHTTP(t *testing.T, endpoint string, status int) string {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	var ultimo string
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == status {
				return string(body)
			}
			ultimo = fmt.Sprintf("status %d: %s", resp.StatusCode, body)
		} else {
			ultimo = err.Error()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s no respondió %d a tiempo (último intento: %s)", endpoint, status, ultimo)
	return ""
}

func esperarFin(t *testing.T, resultado <-chan resultadoRun) error {
	t.Helper()

	select {
	case r := <-resultado:
		return r.err
	case <-time.After(20 * time.Second):
		t.Fatal("run no terminó a tiempo")
		return nil
	}
}

// Con el broker caído el servicio arranca en modo degradado: sirve HTTP en los dos
// puertos, informa que el consumidor no está conectado y se apaga limpiamente al
// cancelarse el contexto.
func TestRunArrancaEnModoDegradadoYSeApagaAlCancelarElContexto(t *testing.T) {
	cfg := configValida(t)
	cfg.DatabaseURL = baseAislada(t)

	cancel, resultado := lanzarRun(t, cfg)

	esperarHTTP(t, "http://127.0.0.1:"+cfg.Port+"/health", http.StatusOK)

	body := esperarHTTP(t, "http://127.0.0.1:"+cfg.Port+"/health/deps", http.StatusServiceUnavailable)
	var deps struct {
		Status string            `json:"status"`
		Deps   map[string]string `json:"deps"`
	}
	if err := json.Unmarshal([]byte(body), &deps); err != nil {
		t.Fatalf("respuesta de /health/deps ilegible: %v (%s)", err, body)
	}
	if deps.Status != "degraded" || deps.Deps["user_deleted_consumer"] == "up" {
		t.Fatalf("con el broker caído el consumidor debía figurar caído: %s", body)
	}

	// El router interno exige el token de servicio: sin él, 401.
	resp, err := http.Post("http://127.0.0.1:"+cfg.InternalPort+"/internal/resolve-owner", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("el servidor interno no responde: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("el router interno sin token debía responder 401, obtuvo %d", resp.StatusCode)
	}

	cancel()
	if err := esperarFin(t, resultado); err != nil {
		t.Fatalf("el apagado ordenado no debía devolver error: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+cfg.Port, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("tras el apagado el puerto público no debía seguir aceptando conexiones")
	}
}

// Con EPHEMERAL_DB el esquema se reinicia antes de migrar: lo que había se pierde.
func TestRunConEphemeralDBReiniciaElEsquema(t *testing.T) {
	cfg := configValida(t)
	cfg.DatabaseURL = baseAislada(t)
	cfg.EphemeralDB = true

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("no se pudo abrir la base aislada: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.Exec("CREATE TABLE sobrante (id int)").Error; err != nil {
		t.Fatalf("no se pudo preparar la tabla previa: %v", err)
	}

	cancel, resultado := lanzarRun(t, cfg)
	esperarHTTP(t, "http://127.0.0.1:"+cfg.Port+"/health", http.StatusOK)
	cancel()
	if err := esperarFin(t, resultado); err != nil {
		t.Fatalf("run devolvió error: %v", err)
	}

	var existe bool
	if err := db.Raw("SELECT to_regclass('public.sobrante') IS NOT NULL").Scan(&existe).Error; err != nil {
		t.Fatalf("no se pudo consultar el esquema: %v", err)
	}
	if existe {
		t.Fatal("EPHEMERAL_DB=true debía borrar las tablas previas")
	}
	var migrada bool
	if err := db.Raw("SELECT to_regclass('public.vehicles') IS NOT NULL").Scan(&migrada).Error; err != nil {
		t.Fatalf("no se pudo consultar el esquema: %v", err)
	}
	if !migrada {
		t.Fatal("tras reiniciar el esquema debía migrarse la tabla vehicles")
	}
}

// Si un puerto ya está ocupado, run no se queda esperando: devuelve el error de
// arranque del servidor HTTP.
func TestRunFallaSiElPuertoYaEstaOcupado(t *testing.T) {
	cfg := configValida(t)
	cfg.DatabaseURL = baseAislada(t)

	ocupado, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		t.Fatalf("no se pudo ocupar el puerto: %v", err)
	}
	defer ocupado.Close()

	_, resultado := lanzarRun(t, cfg)

	err = esperarFin(t, resultado)
	if err == nil || !strings.Contains(err.Error(), "error al iniciar el servidor HTTP") {
		t.Fatalf("esperaba el error de arranque del servidor HTTP, obtuvo: %v", err)
	}
}

// Si el rol de la base no puede migrar el esquema, run falla en el arranque en
// lugar de servir contra tablas inexistentes. Con EPHEMERAL_DB el fallo ocurre antes,
// al intentar reiniciarlo.
func TestRunFallaSiNoPuedeMigrarNiReiniciarElEsquema(t *testing.T) {
	casos := []struct {
		nombre    string
		efimera   bool
		esperaMsg string
	}{
		{"migrar el esquema", false, "no se pudo migrar el esquema"},
		{"reiniciar el esquema efímero", true, "no se pudo reiniciar el esquema efímero"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			cfg := configValida(t)
			cfg.DatabaseURL = dsnSinPermisos(t, baseAislada(t))
			cfg.EphemeralDB = caso.efimera

			err := run(context.Background(), cfg)
			if err == nil || !strings.Contains(err.Error(), caso.esperaMsg) {
				t.Fatalf("esperaba el error %q, obtuvo: %v", caso.esperaMsg, err)
			}
			if strings.Contains(err.Error(), "sin-permisos") {
				t.Fatalf("el error no debe incluir la contraseña de la base: %v", err)
			}
		})
	}
}

// main carga la configuración del entorno, arma el contexto de señales y se apaga
// ordenadamente con SIGTERM, la señal que manda un orquestador al detener el
// servicio. Solo corre donde se puede enviar la señal al propio proceso.
func TestMainSeApagaConSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows no puede enviar SIGTERM al propio proceso")
	}
	cfg := configValida(t)
	cfg.DatabaseURL = baseAislada(t)

	for clave, valor := range map[string]string{
		"PORT":                     cfg.Port,
		"INTERNAL_PORT":            cfg.InternalPort,
		"DATABASE_URL":             cfg.DatabaseURL,
		"VEHICLE_HPKE_PRIVATE_KEY": cfg.VehicleHPKEPrivateKey,
		"PAT_AES_KEY":              cfg.PatAESKey,
		"PAT_HMAC_SECRET":          cfg.PatHMACSecret,
		"ALERT_DEDUP_HMAC_KEY":     base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"IA_HPKE_PUBLIC_KEY":       cfg.IAHPKEPublicKey,
		"AUDIT_PUBLIC_KEY":         cfg.AuditPublicKey,
		"AUDIT_KEY_ID":             cfg.AuditKeyID,
		"INTERNAL_SERVICE_TOKEN":   cfg.InternalServiceToken,
		"RABBITMQ_URL":             brokerInalcanzable,
	} {
		t.Setenv(clave, valor)
	}

	terminado := make(chan struct{})
	go func() {
		defer close(terminado)
		main()
	}()

	// Con el puerto respondiendo, NotifyContext ya está registrado: la señal no
	// mata el proceso de pruebas.
	esperarHTTP(t, "http://127.0.0.1:"+cfg.Port+"/health", http.StatusOK)
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("no se pudo obtener el propio proceso: %v", err)
	}
	if err := self.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("no se pudo enviar SIGTERM: %v", err)
	}

	select {
	case <-terminado:
	case <-time.After(20 * time.Second):
		t.Fatal("main no se apagó tras SIGTERM")
	}
}

// Una configuración inválida corta el arranque con un mensaje que dice qué falló y
// sin filtrar el valor configurado.
func TestRunFallaConConfiguracionInvalida(t *testing.T) {
	casos := []struct {
		nombre string
		rompe  func(cfg *config.Config)
		espera string
	}{
		{"clave privada de Vehicle ilegible", func(c *config.Config) { c.VehicleHPKEPrivateKey = "no-es-base64-secreto" }, "desencriptador HPKE"},
		{"clave pública de IA inválida", func(c *config.Config) { c.IAHPKEPublicKey = "no-es-base64-secreto" }, "cifradores HPKE"},
		{"clave AES inválida", func(c *config.Config) { c.PatAESKey = "no-es-base64-secreto" }, "cifrado at-rest"},
		{"base de datos inalcanzable", func(c *config.Config) {
			c.DatabaseURL = "postgres://vehicle:clave-secreta@127.0.0.1:1/vehicle?sslmode=disable&connect_timeout=1"
		}, "base de datos"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			cfg := configValida(t)
			cfg.DatabaseURL = "postgres://vehicle:clave-secreta@127.0.0.1:1/vehicle?sslmode=disable&connect_timeout=1"
			caso.rompe(cfg)

			err := run(context.Background(), cfg)
			if err == nil {
				t.Fatal("esperaba error de arranque")
			}
			if !strings.Contains(err.Error(), caso.espera) {
				t.Fatalf("el error debía mencionar %q, obtuvo: %v", caso.espera, err)
			}
			for _, secreto := range []string{"no-es-base64-secreto", "clave-secreta"} {
				if strings.Contains(err.Error(), secreto) {
					t.Fatalf("el error no debe incluir valores de configuración sensibles: %v", err)
				}
			}
		})
	}
}

// Si el exchange no existe (error de despliegue: lo declara Core), run no arranca
// en modo degradado: falla. Con el exchange declarado, el consumidor se conecta y
// /health/deps lo informa.
func TestRunConBrokerRealExigeElExchangeYConectaElConsumidor(t *testing.T) {
	brokerURL := os.Getenv("TEST_RABBITMQ_URL")
	if brokerURL == "" {
		t.Skip("TEST_RABBITMQ_URL no configurada: se omite la prueba contra RabbitMQ")
	}
	dsn := baseAislada(t)

	conn, err := amqp.Dial(brokerURL)
	if err != nil {
		t.Fatalf("no se pudo conectar al broker de prueba: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("no se pudo abrir el canal: %v", err)
	}

	id := uuid.NewString()[:8]
	exchange := "vvi.test.run." + id
	queue := "vehicle.run-test-" + id + ".q"

	cfg := configValida(t)
	cfg.DatabaseURL = dsn
	cfg.RabbitMQURL = brokerURL
	cfg.RabbitMQExchange = exchange
	cfg.UserDeletedQueue = queue

	err = run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "no se pudo conectar con RabbitMQ") {
		t.Fatalf("sin el exchange declarado debía fallar el arranque, obtuvo: %v", err)
	}

	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("no se pudo declarar el exchange: %v", err)
	}
	if _, err := ch.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("no se pudo declarar la cola: %v", err)
	}
	t.Cleanup(func() { _ = ch.ExchangeDelete(exchange, false, false) })

	cancel, resultado := lanzarRun(t, cfg)
	esperarHTTP(t, "http://127.0.0.1:"+cfg.Port+"/health/deps", http.StatusOK)
	cancel()
	if err := esperarFin(t, resultado); err != nil {
		t.Fatalf("el apagado ordenado no debía devolver error: %v", err)
	}
}

// awaitDone cede en cuanto la tarea termina y, si no termina, espera solo hasta que
// vence el contexto en lugar de colgar el apagado.
func TestAwaitDone(t *testing.T) {
	t.Run("la tarea termina antes del plazo", func(t *testing.T) {
		done := make(chan struct{})
		close(done)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		inicio := time.Now()
		awaitDone(ctx, done, "no debía loguearse")
		if time.Since(inicio) > 5*time.Second {
			t.Fatal("awaitDone debía volver apenas la tarea terminó")
		}
	})

	t.Run("el plazo vence antes que la tarea", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		terminado := make(chan struct{})
		go func() {
			awaitDone(ctx, make(chan struct{}), "venció el plazo")
			close(terminado)
		}()

		select {
		case <-terminado:
		case <-time.After(5 * time.Second):
			t.Fatal("awaitDone no debía colgarse cuando el contexto venció")
		}
	})
}
