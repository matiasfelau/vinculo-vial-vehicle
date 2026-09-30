package http

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// resolveOwnersUseCase es el contrato que el handler necesita del caso de
// uso, para poder probarlo con un doble sin depender de su implementación
// concreta.
type resolveOwnersUseCase interface {
	ResolveOwners(ctx context.Context, patEnc string) ([]uuid.UUID, error)
}

type resolveOwnerRequest struct {
	PatEnc string `json:"pat_enc" binding:"required"`
}

type resolveOwnerResponse struct {
	OwnerIDs []uuid.UUID `json:"owner_ids"`
}

// ResolveOwnerHandler expone POST /internal/resolve-owner sobre el router
// interno aislado. Solo debe alcanzarlo vinculo-vial-alert, ya autenticado
// por requireServiceToken antes de llegar acá: este handler nunca produce un
// 401 por su cuenta, ni aplica ninguna regla de negocio sobre quién pregunta.
//
// La respuesta es siempre {owner_ids: [...]}, con un array vacío (nunca null)
// cuando la patente no está en la red: es un resultado válido, no un error.
func ResolveOwnerHandler(useCase resolveOwnersUseCase) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req resolveOwnerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("POST /internal/resolve-owner: payload mal formado: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "payload mal formado"})
			return
		}

		ownerIDs, err := useCase.ResolveOwners(c.Request.Context(), req.PatEnc)
		if err != nil {
			writeResolveOwnerError(c, err)
			return
		}
		if ownerIDs == nil {
			ownerIDs = []uuid.UUID{}
		}

		c.JSON(http.StatusOK, resolveOwnerResponse{OwnerIDs: ownerIDs})
	}
}

func writeResolveOwnerError(c *gin.Context, err error) {
	log.Printf("POST /internal/resolve-owner: rechazado por el caso de uso: %v", err)

	switch {
	case errors.Is(err, domain.ErrInvalidPlate), errors.Is(err, domain.ErrInvalidCryptogram):
		c.JSON(http.StatusBadRequest, gin.H{"error": "criptograma o formato de patente inválido"})
	case errors.Is(err, domain.ErrRepositoryUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar la base de datos"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error interno"})
	}
}
