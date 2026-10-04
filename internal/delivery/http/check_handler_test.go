package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

type fakeCheckVehicleUseCase struct {
	exists         bool
	err            error
	receivedPatEnc string
	fueInvocado    bool
}

func (f *fakeCheckVehicleUseCase) Check(ctx context.Context, patEnc string) (bool, error) {
	f.fueInvocado = true
	f.receivedPatEnc = patEnc
	return f.exists, f.err
}

func checkRequest(t *testing.T, body any) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("no se pudo serializar el cuerpo de la solicitud: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vehicles/check", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCheckVehiclePatenteExistenteRetorna200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &fakeCheckVehicleUseCase{exists: true}
	router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": "criptograma"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
	if checker.receivedPatEnc != "criptograma" {
		t.Fatal("el pat_enc recibido no coincide con el enviado al caso de uso")
	}
}

func TestCheckVehiclePatenteInexistenteRetorna404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &fakeCheckVehicleUseCase{exists: false}
	router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": "criptograma"}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d", rec.Code)
	}
}

func TestCheckVehiclePayloadMalFormadoRetorna400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &fakeCheckVehicleUseCase{}
	router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": ""}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
	}
	if checker.fueInvocado {
		t.Fatal("no debía invocarse el caso de uso con payload mal formado")
	}
}

func TestCheckVehicleErroresDelCasoDeUsoRetorna400(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
	}{
		{"patente invalida", domain.ErrInvalidPlate},
		{"criptograma invalido", domain.ErrInvalidCryptogram},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			checker := &fakeCheckVehicleUseCase{err: c.err}
			router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": "criptograma"}))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
			}
		})
	}
}

func TestCheckVehicleBaseDeDatosInalcanzableRetorna503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &fakeCheckVehicleUseCase{err: domain.ErrRepositoryUnavailable}
	router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": "criptograma"}))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperaba 503, obtuvo %d", rec.Code)
	}
}

func TestCheckVehicleErrorInternoRetorna500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &fakeCheckVehicleUseCase{err: errors.New("fallo interno de prueba")}
	router := NewRouter(&fakeRegisterVehicleUseCase{}, checker, &fakePingable{}, &fakeCircuitReporter{estado: "closed"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, checkRequest(t, map[string]string{"pat_enc": "criptograma"}))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("esperaba 500, obtuvo %d", rec.Code)
	}
}
