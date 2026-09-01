package main

import (
	"log"
	"time"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/client/dnrpa"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/client/identity"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/symmetric"
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/lookup"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
	"github.com/matiasfelau/vinculo-vial-vehicle/pkg/config"
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
	repository := postgres.NewVehicleRepository(db)

	identityClient := identity.NewClient(cfg.IdentityInternalURL, cfg.InternalServiceToken, time.Duration(cfg.IdentityTimeoutMS)*time.Millisecond)

	registerVehicleService := vehicle.NewRegisterVehicleService(
		repository,
		identityClient,
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

	errCh := make(chan error, 2)
	go func() {
		log.Printf("vehicle service (público) escuchando en el puerto %s", cfg.Port)
		errCh <- router.Run(":" + cfg.Port)
	}()
	go func() {
		log.Printf("Servidor interno iniciado en puerto %s", cfg.InternalPort)
		errCh <- internalRouter.Run(":" + cfg.InternalPort)
	}()

	if err := <-errCh; err != nil {
		log.Fatalf("error al iniciar el servidor HTTP: %v", err)
	}
}
