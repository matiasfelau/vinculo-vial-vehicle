package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
)

// registerVehicleUseCase es el contrato que el handler necesita del caso de uso,
// para poder probarlo con un doble sin depender de sus implementaciones concretas.
type registerVehicleUseCase interface {
	Register(ctx context.Context, input vehicle.RegisterVehicleInput) error
}

type registerVehicleRequest struct {
	PayloadEnc string `json:"payload_enc" binding:"required"`
}

// RegisterVehicleHandler expone POST /api/v1/vehicles. Lee X-User-Id (inyectado por
// Kong) como owner_id; nunca confía en un X-User-Id que no provenga del gateway.
func RegisterVehicleHandler(useCase registerVehicleUseCase) gin.HandlerFunc {
	return func(c *gin.Context) {
		ownerID, err := uuid.Parse(c.GetHeader("X-User-Id"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "encabezado X-User-Id ausente o inválido"})
			return
		}

		var req registerVehicleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "payload mal formado"})
			return
		}

		input := vehicle.RegisterVehicleInput{
			OwnerID:    ownerID,
			PayloadEnc: req.PayloadEnc,
		}

		if err := useCase.Register(c.Request.Context(), input); err != nil {
			writeRegisterVehicleError(c, err)
			return
		}

		c.Status(http.StatusCreated)
	}
}

func writeRegisterVehicleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrOwnershipMismatch):
		c.JSON(http.StatusForbidden, gin.H{"error": "el dni no corresponde al propietario de la sesión"})
	case errors.Is(err, domain.ErrOwnershipUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo verificar la propiedad del vehículo"})
	case errors.Is(err, domain.ErrOwnerLookupUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar la titularidad del vehículo"})
	case errors.Is(err, domain.ErrDuplicateVehicle):
		c.JSON(http.StatusConflict, gin.H{"error": "el vehículo ya está vinculado a este propietario"})
	case errors.Is(err, domain.ErrInvalidPlate), errors.Is(err, domain.ErrInvalidCryptogram):
		c.JSON(http.StatusBadRequest, gin.H{"error": "criptograma o formato de patente inválido"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error interno"})
	}
}
