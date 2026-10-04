package http

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// checkVehicleUseCase es el contrato que el handler necesita del caso de uso,
// para poder probarlo con un doble sin depender de su implementación concreta.
type checkVehicleUseCase interface {
	Check(ctx context.Context, patEnc string) (bool, error)
}

type checkVehicleRequest struct {
	PatEnc string `json:"pat_enc" binding:"required"`
}

// CheckVehicleHandler expone POST /api/v1/vehicles/check.
//
// A diferencia de RegisterVehicleHandler, no lee X-User-Id: la consulta no
// distingue quién pregunta, solo si la patente está en la red. Kong ya aplicó
// JWT y Rate Limiting por X-User-Id antes de que la solicitud llegue acá; esa
// es la única defensa de este endpoint contra fuerza bruta, así que la
// respuesta es estrictamente 200/404, sin body con detalle.
func CheckVehicleHandler(useCase checkVehicleUseCase) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req checkVehicleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("POST /api/v1/vehicles/check: payload mal formado: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "payload mal formado"})
			return
		}

		exists, err := useCase.Check(c.Request.Context(), req.PatEnc)
		if err != nil {
			writeCheckVehicleError(c, err)
			return
		}

		if !exists {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusOK)
	}
}

func writeCheckVehicleError(c *gin.Context, err error) {
	log.Printf("POST /api/v1/vehicles/check: rechazado por el caso de uso: %v", err)

	switch {
	case errors.Is(err, domain.ErrInvalidPlate), errors.Is(err, domain.ErrInvalidCryptogram):
		c.JSON(http.StatusBadRequest, gin.H{"error": "criptograma o formato de patente inválido"})
	case errors.Is(err, domain.ErrRepositoryUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar la base de datos"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error interno"})
	}
}
