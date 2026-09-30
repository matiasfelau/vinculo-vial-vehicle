package main

import (
	"context"
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
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/lifecycle"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/lookup"
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
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("no se pudo cargar la configuración: %v", err)
	}

	payloadDecryptor, err := hpke.NewDecryptor(cfg.VehicleHPKEPrivateKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el desencriptador HPKE: %v", err)
	}

	dniEncryptor, err := hpke.NewEncryptor(cfg.IdentityHPKEPublicKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el cifrador HPKE hacia identity: %v", err)
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

	checkVehicleService := lookup.NewCheckVehicleService(repository, payloadDecryptor, []byte(cfg.PatHMACSecret))
	resolveOwnersService := lookup.NewResolveOwnersService(repository, payloadDecryptor, []byte(cfg.PatHMACSecret))

	router := httpdelivery.NewRouter(registerVehicleService, checkVehicleService)
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

	if err := lifecycle.RunUntilSignal(ctx, []lifecycle.Server{publicServer, internalServer}, shutdownTimeout); err != nil {
		log.Fatalf("error al iniciar el servidor HTTP: %v", err)
	}
}
