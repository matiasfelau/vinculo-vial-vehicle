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
	ResolveOwners(ctx context.Context, patEnc string, alertID uuid.UUID) ([]uuid.UUID, string, error)
}

type resolveOwnerRequest struct {
	PatEnc  string `json:"pat_enc" binding:"required"`
	AlertID string `json:"alert_id" binding:"required"`
}

// resolveOwnerResponse es la respuesta de /internal/resolve-owner. PatEncIA se
// omite cuando owner_ids está vacío: sin dueños no hay nada que cifrar hacia IA.
type resolveOwnerResponse struct {
	OwnerIDs []uuid.UUID `json:"owner_ids"`
	PatEncIA string      `json:"pat_enc_ia,omitempty"`
}

// ResolveOwnerHandler expone POST /internal/resolve-owner sobre el router
// interno aislado. Solo debe alcanzarlo vinculo-vial-alert, ya autenticado
// por requireServiceToken antes de llegar acá: este handler nunca produce un
// 401 por su cuenta, ni aplica ninguna regla de negocio sobre quién pregunta.
//
// El request exige pat_enc y alert_id (UUID). La respuesta es siempre
// {owner_ids: [...], pat_enc_ia: "..."}, con un array vacío (nunca null) y sin
// pat_enc_ia cuando la patente no está en la red: es un resultado válido, no un
// error.
func ResolveOwnerHandler(useCase resolveOwnersUseCase) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req resolveOwnerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("POST /internal/resolve-owner: payload mal formado: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "payload mal formado"})
			return
		}

		alertID, err := uuid.Parse(req.AlertID)
		if err != nil {
			log.Printf("POST /internal/resolve-owner: alert_id no es un UUID válido")
			c.JSON(http.StatusBadRequest, gin.H{"error": "alert_id inválido"})
			return
		}

		ownerIDs, patEncIA, err := useCase.ResolveOwners(c.Request.Context(), req.PatEnc, alertID)
		if err != nil {
			writeResolveOwnerError(c, err)
			return
		}
		if ownerIDs == nil {
			ownerIDs = []uuid.UUID{}
		}

		c.JSON(http.StatusOK, resolveOwnerResponse{OwnerIDs: ownerIDs, PatEncIA: patEncIA})
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
