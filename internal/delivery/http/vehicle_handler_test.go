package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
)

// fakeVehicleUseCase es el doble del caso de uso: devuelve lo configurado y
// registra con qué lo invocaron.
type fakeVehicleUseCase struct {
	summary vehicle.Summary
	err     error

	registerInput vehicle.PlateInput
	unlinkInput   vehicle.PlateInput
	countOwner    uuid.UUID
	unlinkAllFor  uuid.UUID
	invocaciones  int
}

func (f *fakeVehicleUseCase) Register(ctx context.Context, input vehicle.PlateInput) (vehicle.Summary, error) {
	f.invocaciones++
	f.registerInput = input
	return f.summary, f.err
}

func (f *fakeVehicleUseCase) Unlink(ctx context.Context, input vehicle.PlateInput) (vehicle.Summary, error) {
	f.invocaciones++
	f.unlinkInput = input
	return f.summary, f.err
}

func (f *fakeVehicleUseCase) UnlinkAll(ctx context.Context, ownerID uuid.UUID) error {
	f.invocaciones++
	f.unlinkAllFor = ownerID
	return f.err
}

func (f *fakeVehicleUseCase) Count(ctx context.Context, ownerID uuid.UUID) (vehicle.Summary, error) {
	f.invocaciones++
	f.countOwner = ownerID
	return f.summary, f.err
}

func nuevoRouter(useCase vehicleUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(useCase, &fakePingable{})
}

// peticion arma una solicitud a la API de vehículos con X-User-Id opcional.
func peticion(t *testing.T, metodo, ruta, ownerID string, body any) *http.Request {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("no se pudo serializar el cuerpo de la solicitud: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(metodo, ruta, reader)
	req.Header.Set("Content-Type", "application/json")
	if ownerID != "" {
		req.Header.Set("X-User-Id", ownerID)
	}
	return req
}

func cuerpoValido() plateRequest {
	return plateRequest{PayloadEnc: "criptograma-payload"}
}

func ejecutar(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodificar(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("el cuerpo no es JSON: %q (%v)", rec.Body.String(), err)
	}
	return body
}

// rutasConPatente son las dos rutas que reciben payload_enc.
var rutasConPatente = map[string]string{
	"vincular":    "/api/v1/vehicles",
	"desvincular": "/api/v1/vehicles/unlink",
}

// todasLasRutas incluye las que no llevan cuerpo.
var todasLasRutas = []struct {
	nombre string
	metodo string
	ruta   string
	body   any
}{
	{"vincular", http.MethodPost, "/api/v1/vehicles", cuerpoValido()},
	{"desvincular", http.MethodPost, "/api/v1/vehicles/unlink", cuerpoValido()},
	{"contar", http.MethodGet, "/api/v1/vehicles", nil},
	{"desvincular todos", http.MethodDelete, "/api/v1/vehicles", nil},
}

func TestRutasDeVehiculosSinXUserIdOInvalidoRetornan401ConCodigo(t *testing.T) {
	for _, r := range todasLasRutas {
		for _, header := range []string{"", "no-es-un-uuid"} {
			t.Run(r.nombre+"/"+header, func(t *testing.T) {
				useCase := &fakeVehicleUseCase{}

				rec := ejecutar(nuevoRouter(useCase), peticion(t, r.metodo, r.ruta, header, r.body))

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
				}
				if code := decodificar(t, rec)["code"]; code != "UNAUTHORIZED" {
					t.Fatalf("code = %v, esperaba UNAUTHORIZED", code)
				}
				if useCase.invocaciones != 0 {
					t.Fatal("no debía invocarse el caso de uso sin X-User-Id")
				}
			})
		}
	}
}

func TestVincularYDesvincularConPayloadMalFormadoRetornan400InvalidPlate(t *testing.T) {
	for nombre, ruta := range rutasConPatente {
		for caso, body := range map[string]string{
			"payload_enc vacio": `{"payload_enc": ""}`,
			"sin payload_enc":   `{}`,
			"no es json":        `no-es-json`,
			"tipo incorrecto":   `{"payload_enc": 123}`,
		} {
			t.Run(nombre+"/"+caso, func(t *testing.T) {
				useCase := &fakeVehicleUseCase{}
				req := httptest.NewRequest(http.MethodPost, ruta, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-User-Id", uuid.NewString())

				rec := ejecutar(nuevoRouter(useCase), req)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
				}
				if code := decodificar(t, rec)["code"]; code != "INVALID_PLATE" {
					t.Fatalf("code = %v, esperaba INVALID_PLATE", code)
				}
				if useCase.invocaciones != 0 {
					t.Fatal("no debía invocarse el caso de uso con payload mal formado")
				}
			})
		}
	}
}

func TestVincularExitoRetorna201ConConteoYMaximoYSinPatentes(t *testing.T) {
	useCase := &fakeVehicleUseCase{summary: vehicle.Summary{Count: 1, Max: 3}}
	ownerID := uuid.New()

	rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodPost, "/api/v1/vehicles", ownerID.String(), cuerpoValido()))

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d", rec.Code)
	}
	if body := rec.Body.String(); strings.TrimSpace(body) != `{"count":1,"max":3}` {
		t.Fatalf("cuerpo = %s, esperaba solo count y max", body)
	}
	if useCase.registerInput.OwnerID != ownerID || useCase.registerInput.PayloadEnc != "criptograma-payload" {
		t.Fatalf("el caso de uso recibió %+v", useCase.registerInput)
	}
}

func TestDesvincularExitoRetorna200ConConteoYMaximo(t *testing.T) {
	useCase := &fakeVehicleUseCase{summary: vehicle.Summary{Count: 0, Max: 3}}
	ownerID := uuid.New()

	rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodPost, "/api/v1/vehicles/unlink", ownerID.String(), cuerpoValido()))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
	if body := rec.Body.String(); strings.TrimSpace(body) != `{"count":0,"max":3}` {
		t.Fatalf("cuerpo = %s", body)
	}
	if useCase.unlinkInput.OwnerID != ownerID || useCase.unlinkInput.PayloadEnc != "criptograma-payload" {
		t.Fatalf("el caso de uso recibió %+v", useCase.unlinkInput)
	}
}

func TestContarRetorna200ConConteoDelUsuarioDeLaSesion(t *testing.T) {
	useCase := &fakeVehicleUseCase{summary: vehicle.Summary{Count: 2, Max: 3}}
	ownerID := uuid.New()

	rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodGet, "/api/v1/vehicles", ownerID.String(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
	if body := rec.Body.String(); strings.TrimSpace(body) != `{"count":2,"max":3}` {
		t.Fatalf("cuerpo = %s", body)
	}
	if useCase.countOwner != ownerID {
		t.Fatalf("se contó para %s, esperaba %s", useCase.countOwner, ownerID)
	}
}

func TestDesvincularTodosRetorna204SinCuerpo(t *testing.T) {
	useCase := &fakeVehicleUseCase{}
	ownerID := uuid.New()

	rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodDelete, "/api/v1/vehicles", ownerID.String(), nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperaba 204, obtuvo %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("el 204 no debe llevar cuerpo, llevó %q", rec.Body.String())
	}
	if useCase.unlinkAllFor != ownerID {
		t.Fatalf("se desvincularon los vehículos de %s, esperaba %s", useCase.unlinkAllFor, ownerID)
	}
}

func TestErroresDelCasoDeUsoSeTraducenAStatusYCodeDelContrato(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
		status int
		code   string
	}{
		{"criptograma invalido", domain.ErrInvalidCryptogram, http.StatusBadRequest, "INVALID_PLATE"},
		{"formato de patente invalido", domain.ErrInvalidPlate, http.StatusBadRequest, "INVALID_PLATE"},
		{"patente no vinculada", domain.ErrVehicleNotFound, http.StatusNotFound, "VEHICLE_NOT_FOUND"},
		{"patente duplicada", domain.ErrDuplicateVehicle, http.StatusConflict, "DUPLICATE_VEHICLE"},
		{"tope alcanzado", domain.ErrVehicleLimitReached, http.StatusConflict, "VEHICLE_LIMIT_REACHED"},
		{"base de datos caida", domain.ErrRepositoryUnavailable, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"},
		{"error inesperado", context.Canceled, http.StatusInternalServerError, "INTERNAL_ERROR"},
	}

	for _, r := range todasLasRutas {
		for _, c := range casos {
			t.Run(r.nombre+"/"+c.nombre, func(t *testing.T) {
				useCase := &fakeVehicleUseCase{err: c.err}

				rec := ejecutar(nuevoRouter(useCase), peticion(t, r.metodo, r.ruta, uuid.NewString(), r.body))

				if rec.Code != c.status {
					t.Fatalf("esperaba %d, obtuvo %d", c.status, rec.Code)
				}
				body := decodificar(t, rec)
				if body["code"] != c.code {
					t.Fatalf("code = %v, esperaba %s", body["code"], c.code)
				}
				if msg, _ := body["error"].(string); msg == "" {
					t.Fatal("todo error lleva un mensaje en error")
				}
			})
		}
	}
}

func TestBaseCaidaRespondeConRetryAfterYSoloEllaLoLleva(t *testing.T) {
	useCase := &fakeVehicleUseCase{err: domain.ErrRepositoryUnavailable}
	rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodPost, "/api/v1/vehicles", uuid.NewString(), cuerpoValido()))
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("el 503 debe llevar Retry-After")
	}

	useCase = &fakeVehicleUseCase{err: domain.ErrDuplicateVehicle}
	rec = ejecutar(nuevoRouter(useCase), peticion(t, http.MethodPost, "/api/v1/vehicles", uuid.NewString(), cuerpoValido()))
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("solo el 503 lleva Retry-After")
	}
}

// Ni la respuesta ni el log contienen la patente ni el criptograma: el handler solo
// maneja payload_enc, y ante un error loguea el del caso de uso, que no la cita.
func TestNingunaRespuestaNiLogContienenLaPatenteNiElCriptograma(t *testing.T) {
	var logs bytes.Buffer
	anterior := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(anterior) })

	const patente, criptograma = "AB123CD", "CRIPTOGRAMA-SECRETO"
	errores := []error{domain.ErrInvalidPlate, domain.ErrInvalidCryptogram, domain.ErrDuplicateVehicle, domain.ErrVehicleLimitReached, domain.ErrVehicleNotFound, domain.ErrRepositoryUnavailable, context.Canceled}

	for _, err := range errores {
		for nombre, ruta := range rutasConPatente {
			useCase := &fakeVehicleUseCase{err: err}
			rec := ejecutar(nuevoRouter(useCase), peticion(t, http.MethodPost, ruta, uuid.NewString(), map[string]string{"payload_enc": criptograma}))
			if strings.Contains(rec.Body.String(), patente) || strings.Contains(rec.Body.String(), criptograma) {
				t.Fatalf("%s: la respuesta contiene datos sensibles: %s", nombre, rec.Body.String())
			}
		}
	}

	// Un cuerpo mal formado que cita la patente tampoco puede llegar al log.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vehicles", strings.NewReader(`{"payload_enc": 123, "patente": "`+patente+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", uuid.NewString())
	rec := ejecutar(nuevoRouter(&fakeVehicleUseCase{}), req)
	if strings.Contains(rec.Body.String(), patente) {
		t.Fatalf("la respuesta cita el cuerpo recibido: %s", rec.Body.String())
	}

	if registro := logs.String(); strings.Contains(registro, patente) || strings.Contains(registro, criptograma) {
		t.Fatalf("el log contiene datos sensibles: %s", registro)
	}
	if logs.Len() == 0 {
		t.Fatal("la prueba no capturó ningún log: no verifica nada")
	}
}

func TestLasRespuestasExitosasNoTienenOtrosCamposQueCountYMax(t *testing.T) {
	useCase := &fakeVehicleUseCase{summary: vehicle.Summary{Count: 3, Max: 3}}

	for _, r := range todasLasRutas[:3] {
		rec := ejecutar(nuevoRouter(useCase), peticion(t, r.metodo, r.ruta, uuid.NewString(), r.body))
		body := decodificar(t, rec)
		if len(body) != 2 || body["count"] == nil || body["max"] == nil {
			t.Fatalf("%s: la respuesta debe tener solo count y max, obtuvo %v", r.nombre, body)
		}
	}
}
