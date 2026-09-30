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
	token      string
	httpClient *http.Client
}

// NewClient construye el cliente con un timeout explícito para la llamada a Identity.
// token se envía en cada solicitud como X-Internal-Service-Token, exigido por Identity
// para autorizar el tráfico de servicio a servicio en la red interna (Zero-Trust).
func NewClient(url string, token string, timeout time.Duration) *Client {
	return &Client{
		url:        url,
		token:      token,
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
	req.Header.Set("X-Internal-Service-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// El destino nunca respondió: timeout o error de conexión. Cuenta como falla
		// de disponibilidad (503) para el circuit breaker de esta dependencia.
		return false, fmt.Errorf("%w: %v", domain.ErrOwnershipUnavailable, err)
	}
	defer resp.Body.Close()

	// Un 5xx significa que identity fue alcanzado pero falló al procesar la solicitud:
	// es un error de la dependencia, no de disponibilidad de red (502), y también
	// cuenta como falla para el breaker.
	if resp.StatusCode >= http.StatusInternalServerError {
		return false, fmt.Errorf("%w: identity respondió con estado %d", domain.ErrOwnershipUpstreamFailure, resp.StatusCode)
	}

	// Un 4xx indica que esta solicitud fue rechazada por su contenido (error de
	// configuración o de negocio propio), no una indisponibilidad de identity: no se
	// envuelve en ninguno de los dos sentinels anteriores y por lo tanto no cuenta
	// como falla para el breaker.
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("domain: identity rechazó la solicitud con estado %d", resp.StatusCode)
	}

	var respBody verifyOwnershipResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		// El destino respondió 200 pero con un cuerpo inválido: mismo tratamiento que
		// un 5xx, el destino fue alcanzado pero la respuesta no es utilizable.
		return false, fmt.Errorf("%w: respuesta inválida: %v", domain.ErrOwnershipUpstreamFailure, err)
	}

	return respBody.Match, nil
}
