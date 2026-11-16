package http

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// dedupKeyUseCase es el contrato que el handler necesita del caso de uso, para
// poder probarlo con un doble sin depender de su implementación concreta.
type dedupKeyUseCase interface {
	DedupKey(patEnc, issuerID string) (string, error)
}

type dedupKeyRequest struct {
	PatEnc   string `json:"pat_enc" binding:"required"`
	IssuerID string `json:"issuer_id" binding:"required"`
}

type dedupKeyResponse struct {
	DedupKey string `json:"dedup_key"`
}

// DedupKeyHandler expone POST /internal/dedup-key (US-16) sobre el router interno
// aislado: Alert lo usa en el intento, antes de que el emisor tenga que capturar
// fotos, para saber si ya ocupó su cupo de una alerta por patente cada 24 h.
//
// El request exige pat_enc e issuer_id. La respuesta es {dedup_key}, o 400
// INVALID_PLATE si el criptograma no abre o la patente no tiene el formato esperado.
// No consulta la base ni reserva nada: la respuesta es igual con o sin dueños, así que
// no revela si la patente está en la red. Ni la patente, ni pat_enc, ni la dedup_key
// se loguean.
func DedupKeyHandler(useCase dedupKeyUseCase) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dedupKeyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("POST /internal/dedup-key: payload mal formado: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "payload mal formado"})
			return
		}

		dedupKey, err := useCase.DedupKey(req.PatEnc, req.IssuerID)
		if err != nil {
			log.Printf("POST /internal/dedup-key: rechazado por el caso de uso: %v", err)
			if errors.Is(err, domain.ErrInvalidPlate) || errors.Is(err, domain.ErrInvalidCryptogram) {
				writeError(c, http.StatusBadRequest, codeInvalidPlate, "criptograma o formato de patente inválido")
				return
			}
			writeError(c, http.StatusInternalServerError, codeInternal, "error interno")
			return
		}

		c.JSON(http.StatusOK, dedupKeyResponse{DedupKey: dedupKey})
	}
}
