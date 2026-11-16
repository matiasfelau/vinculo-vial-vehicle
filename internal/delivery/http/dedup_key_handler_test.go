package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func dedupKeyHTTPRequest(t *testing.T, body any, token string) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("no se pudo serializar el cuerpo de la solicitud: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/dedup-key", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(HeaderInternalServiceToken, token)
	}
	return req
}

func TestDedupKeyDevuelve200ConLaClaveYPasaLosCamposAlCasoDeUso(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeResolveOwnersUseCase{dedupKey: "ab12"}
	router := NewInternalRouter(secretoDePrueba, useCase)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, dedupKeyHTTPRequest(t, map[string]string{"pat_enc": "criptograma", "issuer_id": "emisor-1"}, secretoDePrueba))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if len(res) != 1 || res["dedup_key"] != "ab12" {
		t.Fatalf("esperaba exactamente {dedup_key: ab12}, obtuvo %s", rec.Body.String())
	}
	if useCase.dedupKeyPatEnc != "criptograma" || useCase.dedupKeyIssuer != "emisor-1" {
		t.Fatalf("el caso de uso recibió pat_enc=%q issuer_id=%q", useCase.dedupKeyPatEnc, useCase.dedupKeyIssuer)
	}
}

func TestDedupKeyExigeElTokenDeServicio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeResolveOwnersUseCase{dedupKey: "ab12"}
	router := NewInternalRouter(secretoDePrueba, useCase)

	for nombre, token := range map[string]string{"sin token": "", "token incorrecto": "otro"} {
		t.Run(nombre, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, dedupKeyHTTPRequest(t, map[string]string{"pat_enc": "c", "issuer_id": "e"}, token))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
			}
		})
	}
	if useCase.dedupKeyInvocado {
		t.Fatal("el caso de uso no debe invocarse sin el token de servicio")
	}
}

func TestDedupKeyPayloadMalFormadoDevuelve400SinInvocarElCasoDeUso(t *testing.T) {
	gin.SetMode(gin.TestMode)
	casos := map[string]map[string]string{
		"sin pat_enc":   {"issuer_id": "emisor-1"},
		"sin issuer_id": {"pat_enc": "criptograma"},
		"vacío":         {},
	}

	for nombre, body := range casos {
		t.Run(nombre, func(t *testing.T) {
			useCase := &fakeResolveOwnersUseCase{}
			router := NewInternalRouter(secretoDePrueba, useCase)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, dedupKeyHTTPRequest(t, body, secretoDePrueba))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
			}
			if useCase.dedupKeyInvocado {
				t.Fatal("el caso de uso no debe invocarse con un payload mal formado")
			}
		})
	}
}

func TestDedupKeyConPatenteInvalidaDevuelve400InvalidPlate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for nombre, errCaso := range map[string]error{
		"formato inválido":     domain.ErrInvalidPlate,
		"criptograma inválido": domain.ErrInvalidCryptogram,
	} {
		t.Run(nombre, func(t *testing.T) {
			router := NewInternalRouter(secretoDePrueba, &fakeResolveOwnersUseCase{dedupKeyErr: errCaso})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, dedupKeyHTTPRequest(t, map[string]string{"pat_enc": "c", "issuer_id": "e"}, secretoDePrueba))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
			}
			var res map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("la respuesta no es JSON: %v", err)
			}
			if res["code"] != "INVALID_PLATE" {
				t.Fatalf("esperaba code INVALID_PLATE, obtuvo %s", rec.Body.String())
			}
			if _, hay := res["dedup_key"]; hay {
				t.Fatal("un error no debe devolver dedup_key")
			}
		})
	}
}

func TestDedupKeyErrorInesperadoDevuelve500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba, &fakeResolveOwnersUseCase{dedupKeyErr: errors.New("falla-interna")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, dedupKeyHTTPRequest(t, map[string]string{"pat_enc": "c", "issuer_id": "e"}, secretoDePrueba))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("esperaba 500, obtuvo %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "falla-interna") {
		t.Fatalf("el 500 no debe filtrar el detalle del error: %s", rec.Body.String())
	}
}

func TestDedupKeyNoLogueaPatEncNiLaClave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	for _, useCase := range []*fakeResolveOwnersUseCase{
		{dedupKey: "clave-secreta-ab12"},
		{dedupKeyErr: domain.ErrInvalidPlate},
	} {
		router := NewInternalRouter(secretoDePrueba, useCase)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, dedupKeyHTTPRequest(t, map[string]string{"pat_enc": "cripto-secreto", "issuer_id": uuid.NewString()}, secretoDePrueba))
	}

	for _, secreto := range []string{"cripto-secreto", "clave-secreta-ab12"} {
		if strings.Contains(buf.String(), secreto) {
			t.Fatalf("el log contiene %q: %s", secreto, buf.String())
		}
	}
}
