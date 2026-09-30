package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
)

type fakeRegisterVehicleUseCase struct {
	err           error
	receivedInput vehicle.RegisterVehicleInput
	fueInvocado   bool
}

func (f *fakeRegisterVehicleUseCase) Register(ctx context.Context, input vehicle.RegisterVehicleInput) error {
	f.fueInvocado = true
	f.receivedInput = input
	return f.err
}

func vehiclesRequest(t *testing.T, ownerID string, body any) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("no se pudo serializar el cuerpo de la solicitud: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vehicles", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if ownerID != "" {
		req.Header.Set("X-User-Id", ownerID)
	}
	return req
}

func cuerpoValido() registerVehicleRequest {
	return registerVehicleRequest{PayloadEnc: "criptograma-payload"}
}

func TestRegisterVehicleSinXUserIdRetorna401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeRegisterVehicleUseCase{}
	router := NewRouter(useCase, &fakeCheckVehicleUseCase{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, vehiclesRequest(t, "", cuerpoValido()))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
	if useCase.fueInvocado {
		t.Fatal("no debía invocarse el caso de uso sin X-User-Id")
	}
}

func TestRegisterVehicleConXUserIdInvalidoRetorna401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeRegisterVehicleUseCase{}
	router := NewRouter(useCase, &fakeCheckVehicleUseCase{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, vehiclesRequest(t, "no-es-un-uuid", cuerpoValido()))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
}

func TestRegisterVehiclePayloadMalFormadoRetorna400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeRegisterVehicleUseCase{}
	router := NewRouter(useCase, &fakeCheckVehicleUseCase{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, vehiclesRequest(t, uuid.NewString(), map[string]string{"payload_enc": ""}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
	}
	if useCase.fueInvocado {
		t.Fatal("no debía invocarse el caso de uso con payload mal formado")
	}
}

func TestRegisterVehicleExitoRetorna201(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useCase := &fakeRegisterVehicleUseCase{}
	router := NewRouter(useCase, &fakeCheckVehicleUseCase{})

	ownerID := uuid.New()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, vehiclesRequest(t, ownerID.String(), cuerpoValido()))

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d", rec.Code)
	}
	if useCase.receivedInput.OwnerID != ownerID {
		t.Fatal("el owner_id extraído de X-User-Id no coincide con el enviado al caso de uso")
	}
	if useCase.receivedInput.PayloadEnc != "criptograma-payload" {
		t.Fatal("el payload_enc recibido no coincide con el enviado al caso de uso")
	}
}

func TestRegisterVehicleErroresDelCasoDeUso(t *testing.T) {
	casos := []struct {
		nombre         string
		err            error
		statusEsperado int
	}{
		{"ownership mismatch", domain.ErrOwnershipMismatch, http.StatusForbidden},
		{"identity inalcanzable", domain.ErrOwnershipUnavailable, http.StatusServiceUnavailable},
		{"identity respondió con error de servidor", domain.ErrOwnershipUpstreamFailure, http.StatusBadGateway},
		{"dnrpa inalcanzable", domain.ErrOwnerLookupUnavailable, http.StatusServiceUnavailable},
		{"base de datos inalcanzable", domain.ErrRepositoryUnavailable, http.StatusServiceUnavailable},
		{"vehiculo duplicado", domain.ErrDuplicateVehicle, http.StatusConflict},
		{"patente invalida", domain.ErrInvalidPlate, http.StatusBadRequest},
		{"criptograma invalido", domain.ErrInvalidCryptogram, http.StatusBadRequest},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			useCase := &fakeRegisterVehicleUseCase{err: c.err}
			router := NewRouter(useCase, &fakeCheckVehicleUseCase{})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, vehiclesRequest(t, uuid.NewString(), cuerpoValido()))

			if rec.Code != c.statusEsperado {
				t.Fatalf("esperaba %d, obtuvo %d", c.statusEsperado, rec.Code)
			}
		})
	}
}
