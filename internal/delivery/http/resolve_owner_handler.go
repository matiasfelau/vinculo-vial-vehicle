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
	ResolveOwners(ctx context.Context, patEnc string, alertID uuid.UUID, issuerID string) (domain.OwnerResolution, error)
}

type resolveOwnerRequest struct {
	PatEnc  string `json:"pat_enc" binding:"required"`
	AlertID string `json:"alert_id" binding:"required"`

	// IssuerID (US-16) es opcional: si llega, la respuesta suma dedup_key.
	IssuerID string `json:"issuer_id"`
}

// resolveOwnerResponse es la respuesta de /internal/resolve-owner. PatEncIA va
// siempre, también cuando owner_ids está vacío: IA valida la patente igual.
// PendingRef solo va cuando la patente no tiene dueños (US-10) y se omite si no.
// PatEncAudit y AuditKeyID (US-11) también van siempre: Alert los lleva a Audit
// por el outbox y nunca los guarda en tablas de dominio. DedupKey (US-16) solo va
// cuando el request trae issuer_id y se omite si no.
type resolveOwnerResponse struct {
	OwnerIDs    []uuid.UUID `json:"owner_ids"`
	PatEncIA    string      `json:"pat_enc_ia"`
	PendingRef  *uuid.UUID  `json:"pending_ref,omitempty"`
	PatEncAudit string      `json:"pat_enc_audit"`
	AuditKeyID  string      `json:"audit_key_id"`
	DedupKey    string      `json:"dedup_key,omitempty"`
}

// ResolveOwnerHandler expone POST /internal/resolve-owner sobre el router
// interno aislado. Solo debe alcanzarlo vinculo-vial-alert, ya autenticado
// por requireServiceToken antes de llegar acá: este handler nunca produce un
// 401 por su cuenta, ni aplica ninguna regla de negocio sobre quién pregunta.
//
// El request exige pat_enc y alert_id (UUID). La respuesta es siempre
// {owner_ids: [...], pat_enc_ia: "...", pending_ref?: "...", pat_enc_audit: "...",
// audit_key_id: "..."}, con un array vacío (nunca null) cuando la patente no está
// en la red: es un resultado válido, no un error. pending_ref solo acompaña a un
// owner_ids vacío. issuer_id es opcional (US-16): con él, la respuesta suma
// dedup_key; sin él, queda como siempre.
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

		resolution, err := useCase.ResolveOwners(c.Request.Context(), req.PatEnc, alertID, req.IssuerID)
		if err != nil {
			writeResolveOwnerError(c, err)
			return
		}
		ownerIDs := resolution.OwnerIDs
		if ownerIDs == nil {
			ownerIDs = []uuid.UUID{}
		}

		c.JSON(http.StatusOK, resolveOwnerResponse{
			OwnerIDs:    ownerIDs,
			PatEncIA:    resolution.PatEncIA,
			PendingRef:  resolution.PendingRef,
			PatEncAudit: resolution.PatEncAudit,
			AuditKeyID:  resolution.AuditKeyID,
			DedupKey:    resolution.DedupKey,
		})
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
