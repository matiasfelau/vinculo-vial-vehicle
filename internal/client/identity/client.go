// Package identity implementa el cliente HTTP hacia el endpoint interno de
// verificación de propiedad de vinculo-vial-identity.
package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// Client implementa domain.OwnershipVerifier contra POST /internal/verify-ownership.
// La llamada nunca reintenta indefinidamente: un fallo de Identity nunca debe
// degradar en un alta sin verificar propiedad.
type Client struct {
	url        string
	httpClient *http.Client
}

// NewClient construye el cliente con un timeout explícito para la llamada a Identity.
func NewClient(url string, timeout time.Duration) *Client {
	return &Client{
		url:        url,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type verifyOwnershipRequest struct {
	OwnerID string `json:"owner_id"`
	DNIEnc  string `json:"dni_enc"`
}

type verifyOwnershipResponse struct {
	Match bool `json:"match"`
}

// VerifyOwnership reenvía dniEnc sin abrirlo: este servicio no posee la clave para
// desencriptarlo. Cualquier fallo de red, timeout o respuesta inesperada se reporta
// como error (envuelve domain.ErrOwnershipUnavailable); nunca se asume match implícito.
func (c *Client) VerifyOwnership(ctx context.Context, ownerID uuid.UUID, dniEnc []byte) (bool, error) {
	reqBody, err := json.Marshal(verifyOwnershipRequest{
		OwnerID: ownerID.String(),
		DNIEnc:  base64.StdEncoding.EncodeToString(dniEnc),
	})
	if err != nil {
		return false, fmt.Errorf("%w: no se pudo serializar la solicitud: %v", domain.ErrOwnershipUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return false, fmt.Errorf("%w: no se pudo construir la solicitud: %v", domain.ErrOwnershipUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrOwnershipUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: identity respondió con estado %d", domain.ErrOwnershipUnavailable, resp.StatusCode)
	}

	var respBody verifyOwnershipResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return false, fmt.Errorf("%w: respuesta inválida: %v", domain.ErrOwnershipUnavailable, err)
	}

	return respBody.Match, nil
}
