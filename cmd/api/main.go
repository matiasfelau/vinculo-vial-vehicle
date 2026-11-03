package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/breaker"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/client/dnrpa"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/client/identity"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/symmetric"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/events"
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/lifecycle"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/lookup"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/outbox"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/sweeper"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/userdeleted"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
	"github.com/matiasfelau/vinculo-vial-vehicle/pkg/config"
)

// Plazos del *http.Server de ambos servidores (público e interno). Sin ellos,
// el server de Go por defecto no aplica ningún límite de lectura ni de
// escritura: una conexión lenta o maliciosa retendría el handler y su
// goroutine sin plazo. shutdownTimeout acota, además, cuánto espera el
// apagado ordenado a que las peticiones en vuelo terminen antes de cortarlas.
const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 10 * time.Second
	httpWriteTimeout      = 15 * time.Second
	httpIdleTimeout       = 60 * time.Second
	shutdownTimeout       = 10 * time.Second

	// backgroundShutdownTimeout acota, en conjunto, cuánto espera el apagado al lote
	// en curso del worker de outbox y al mensaje en curso del consumidor de
	// user.deleted.
	backgroundShutdownTimeout = 5 * time.Second
)

// awaitDone espera a que done se cierre o a que ctx venza, y en ese caso loguea
// onTimeout. Los dos consumen el mismo presupuesto de tiempo, así que esperar a
// varias tareas de fondo nunca excede el de ctx.
func awaitDone(ctx context.Context, done <-chan struct{}, onTimeout string) {
	select {
	case <-done:
	case <-ctx.Done():
		log.Println(onTimeout)
	}
}

// newEncryptors construye los dos cifradores HPKE de salida, cada uno con la
// clave pública de su destinatario: dni hacia Identity (IDENTITY_HPKE_PUBLIC_KEY)
// y plate hacia IA (IA_HPKE_PUBLIC_KEY). Una clave inválida hace fallar el arranque.
func newEncryptors(cfg *config.Config) (dni, plate *hpke.Encryptor, err error) {
	dni, err = hpke.NewEncryptor(cfg.IdentityHPKEPublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cifrador hacia identity: %w", err)
	}

	plate, err = hpke.NewEncryptor(cfg.IAHPKEPublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cifrador hacia ia: %w", err)
	}

	return dni, plate, nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("no se pudo cargar la configuración: %v", err)
	}

	payloadDecryptor, err := hpke.NewDecryptor(cfg.VehicleHPKEPrivateKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el desencriptador HPKE: %v", err)
	}

	dniEncryptor, plateEncryptor, err := newEncryptors(cfg)
	if err != nil {
		log.Fatalf("no se pudieron inicializar los cifradores HPKE: %v", err)
	}

	plateCipher, err := symmetric.NewCipher(cfg.PatAESKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el cifrado at-rest: %v", err)
	}

	dnrpaClient := dnrpa.NewMockDNRPAAdapter([]byte(cfg.MockDNRPAOwnerDNI))

	db, err := postgres.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("no se pudo conectar a la base de datos: %v", err)
	}
	if cfg.EphemeralDB {
		log.Println("EPHEMERAL_DB=true: reiniciando el esquema para un despliegue efímero")
		if err := postgres.ResetSchema(db); err != nil {
			log.Fatalf("no se pudo reiniciar el esquema efímero: %v", err)
		}
	}
	if err := postgres.AutoMigrate(db); err != nil {
		log.Fatalf("no se pudo migrar el esquema: %v", err)
	}
	repository := postgres.NewVehicleRepository(db, postgres.WithQueryTimeout(cfg.DBQueryTimeout))
	outboxRepository := postgres.NewOutboxRepository(db, cfg.DBQueryTimeout)

	// Connect solo devuelve error si el exchange no existe (error de despliegue): un
	// broker inalcanzable no hace fallar el arranque. El servicio arranca en modo
	// degradado y reconecta en background; los eventos esperan en el outbox.
	rabbitConn, err := events.Connect(cfg.RabbitMQURL, cfg.RabbitMQExchange)
	if err != nil {
		log.Fatalf("no se pudo conectar con RabbitMQ: %v", err)
	}
	defer rabbitConn.Close()

	// El worker de outbox publica en background: ninguna operación de negocio
	// depende de la disponibilidad del broker.
	outboxWorker := outbox.NewWorker(outboxRepository, events.NewPublisher(rabbitConn, cfg.RabbitMQExchange, cfg.RabbitMQPublishTimeout), cfg.OutboxWorkerBatchSize)

	identityClient := identity.NewClient(cfg.IdentityInternalURL, cfg.InternalServiceToken, time.Duration(cfg.IdentityTimeoutMS)*time.Millisecond)
	identityCircuitBreaker := breaker.New(cfg.IdentityCBFailureThreshold, cfg.IdentityCBWindow, cfg.IdentityCBOpenDuration)
	identityVerifier := identity.NewBreakerVerifier(identityClient, identityCircuitBreaker)

	registerVehicleService := vehicle.NewRegisterVehicleService(
		repository,
		identityVerifier,
		dnrpaClient,
		payloadDecryptor,
		dniEncryptor,
		plateCipher,
		[]byte(cfg.PatHMACSecret),
	)

	resolveOwnersService := lookup.NewResolveOwnersService(repository, payloadDecryptor, plateEncryptor, []byte(cfg.PatHMACSecret), cfg.PendingDeliveryTTL)

	userDeletedService := userdeleted.NewRemoveVehiclesService(repository)
	userDeletedStatus := &events.ConnectionStatus{}

	router := httpdelivery.NewRouter(registerVehicleService, repository, identityVerifier,
		httpdelivery.DependencyCheck{Name: "user_deleted_consumer", Healthy: userDeletedStatus.Connected},
	)
	internalRouter := httpdelivery.NewInternalRouter(cfg.InternalServiceToken, resolveOwnersService)

	publicServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
	internalServer := &http.Server{
		Addr:              ":" + cfg.InternalPort,
		Handler:           internalRouter,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}

	log.Printf("vehicle service (público) escuchando en el puerto %s", cfg.Port)
	log.Printf("Servidor interno iniciado en puerto %s", cfg.InternalPort)

	// El apagado ordenado se dispara ante SIGINT o SIGTERM (la señal que envía
	// un orquestador de contenedores al detener el servicio): las peticiones en
	// vuelo terminan de atenderse antes de cerrar el proceso.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		outboxWorker.Run(workerCtx, cfg.OutboxWorkerInterval)
	}()

	// El sweeper de patentes pendientes vencidas corre hasta la señal de apagado; se
	// espera a que termine antes de cerrar el proceso.
	sweeperDone := make(chan struct{})
	go func() {
		defer close(sweeperDone)
		sweeper.NewPendingSweeper(repository).Run(ctx, cfg.SweeperInterval)
	}()

	// El consumidor de user.deleted (US-11) corre hasta la señal de apagado, con su
	// propia conexión y reconexión: un broker caído no afecta a las rutas HTTP. Al
	// recibir la señal termina el mensaje en curso; el resto vuelve a la cola.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		events.RunWithReconnect(ctx, cfg.RabbitMQURL, cfg.UserDeletedQueue, cfg.RabbitMQPrefetch, userDeletedStatus,
			func(channel events.ConsumerChannel) *events.Consumer {
				deadLetters := events.NewAMQPDeadLetterPublisher(channel, cfg.RabbitMQPublishTimeout)
				handler := events.NewMessageHandler(events.NewUserDeletedProcessor(userDeletedService), deadLetters, cfg.UserDeletedQueue, cfg.MaxRetries)
				return events.NewConsumer(channel, cfg.UserDeletedQueue, handler)
			})
	}()

	if err := lifecycle.RunUntilSignal(ctx, []lifecycle.Server{publicServer, internalServer}, shutdownTimeout); err != nil {
		log.Fatalf("error al iniciar el servidor HTTP: %v", err)
	}

	stop()
	<-sweeperDone

	// El worker se detiene después de drenar los servidores, para que un evento
	// encolado por una petición en vuelo todavía alcance a publicarse. Lo que no
	// llegue queda en el outbox y sale al reiniciar.
	stopWorker()
	background, cancelBackground := context.WithTimeout(context.Background(), backgroundShutdownTimeout)
	defer cancelBackground()
	awaitDone(background, workerDone, "el worker de outbox no terminó a tiempo; los eventos pendientes se reintentan al reiniciar")
	awaitDone(background, consumerDone, "el consumidor de user.deleted no terminó a tiempo; el mensaje en vuelo se reentrega al reiniciar")
}
