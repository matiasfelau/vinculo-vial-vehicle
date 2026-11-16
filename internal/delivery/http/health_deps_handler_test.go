package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func getDepsHealth(t *testing.T, checks ...DependencyCheck) (int, map[string]any) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{}, checks...)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/deps", nil))

	var cuerpo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, cuerpo
}

func consumidor(arriba bool) DependencyCheck {
	return DependencyCheck{Name: "user_deleted_consumer", Healthy: func() bool { return arriba }}
}

func TestDepsHealthSinDependenciasRetorna200Ok(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t)

	if codigo != http.StatusOK || cuerpo["status"] != "ok" {
		t.Fatalf("esperaba 200 ok, obtuvo %d %v", codigo, cuerpo["status"])
	}
	if _, hay := cuerpo["deps"]; hay {
		t.Fatalf("sin dependencias no debía haber deps: %v", cuerpo)
	}
}

func TestDepsHealthYaNoReportaAIdentity(t *testing.T) {
	_, cuerpo := getDepsHealth(t, consumidor(true))

	if _, hay := cuerpo["upstreams"]; hay {
		t.Fatalf("Vehicle ya no depende de Identity: %v", cuerpo)
	}
}

func TestDepsHealthConsumidorConectadoRetorna200YLoReportaArriba(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t, consumidor(true))

	if codigo != http.StatusOK || cuerpo["status"] != "ok" {
		t.Fatalf("esperaba 200 ok, obtuvo %d %v", codigo, cuerpo["status"])
	}
	deps, ok := cuerpo["deps"].(map[string]any)
	if !ok || deps["user_deleted_consumer"] != "up" {
		t.Fatalf("esperaba deps.user_deleted_consumer \"up\", obtuvo %v", cuerpo["deps"])
	}
}

func TestDepsHealthConsumidorDesconectadoRetorna503Degraded(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t, consumidor(false))

	if codigo != http.StatusServiceUnavailable || cuerpo["status"] != "degraded" {
		t.Fatalf("esperaba 503 degraded, obtuvo %d %v", codigo, cuerpo["status"])
	}
	deps, ok := cuerpo["deps"].(map[string]any)
	if !ok || deps["user_deleted_consumer"] != "down" {
		t.Fatalf("esperaba deps.user_deleted_consumer \"down\", obtuvo %v", cuerpo["deps"])
	}
	if mensaje, _ := cuerpo["error"].(string); mensaje != "user_deleted_consumer no disponible" {
		t.Fatalf("error = %q", mensaje)
	}
}

func TestDepsHealthVariasCaidasLasReportaTodas(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t, consumidor(false), DependencyCheck{Name: "otra", Healthy: func() bool { return false }})

	if codigo != http.StatusServiceUnavailable {
		t.Fatalf("esperaba 503, obtuvo %d", codigo)
	}
	if mensaje, _ := cuerpo["error"].(string); mensaje != "user_deleted_consumer no disponible; otra no disponible" {
		t.Fatalf("el error debía nombrar ambas dependencias caídas, obtuvo %q", mensaje)
	}
}

func TestDepsHealthConsumidorDesconectadoNoAfectaReady(t *testing.T) {
	// Un consumidor sin broker no debe sacar a la instancia del balanceo.
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{}, consumidor(false))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba /ready 200, obtuvo %d", rec.Code)
	}
}
