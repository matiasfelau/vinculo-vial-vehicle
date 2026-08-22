package main

import (
	"log"
	"time"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/client/identity"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/symmetric"
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
	"github.com/matiasfelau/vinculo-vial-vehicle/pkg/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("no se pudo cargar la configuración: %v", err)
	}

	plateDecryptor, err := hpke.NewDecryptor(cfg.VehicleHPKEPrivateKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el desencriptador HPKE: %v", err)
	}

	plateCipher, err := symmetric.NewCipher(cfg.PatAESKey)
	if err != nil {
		log.Fatalf("no se pudo inicializar el cifrado at-rest: %v", err)
	}

	db, err := postgres.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("no se pudo conectar a la base de datos: %v", err)
	}
	if err := postgres.AutoMigrate(db); err != nil {
		log.Fatalf("no se pudo migrar el esquema: %v", err)
	}
	repository := postgres.NewVehicleRepository(db)

	identityClient := identity.NewClient(cfg.IdentityInternalURL, time.Duration(cfg.IdentityTimeoutMS)*time.Millisecond)

	registerVehicleService := vehicle.NewRegisterVehicleService(
		repository,
		identityClient,
		plateDecryptor,
		plateCipher,
		[]byte(cfg.PatHMACSecret),
	)

	router := httpdelivery.NewRouter(registerVehicleService)

	log.Printf("vehicle service escuchando en el puerto %s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("error al iniciar el servidor HTTP: %v", err)
	}
}
