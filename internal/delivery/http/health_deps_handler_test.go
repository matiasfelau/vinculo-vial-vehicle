package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeCircuitReporter struct {
	estado string
}

func (f *fakeCircuitReporter) CircuitState() string {
	return f.estado
}

func getDepsHealth(t *testing.T, estado string) (int, map[string]any) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeRegisterVehicleUseCase{}, &fakeCheckVehicleUseCase{}, &fakePingable{}, &fakeCircuitReporter{estado: estado})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/deps", nil))

	var cuerpo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, cuerpo
}

func TestDepsHealthCircuitoCerradoRetorna200Ok(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t, "closed")

	if codigo != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", codigo)
	}
	if cuerpo["status"] != "ok" {
		t.Fatalf("esperaba status \"ok\", obtuvo %v", cuerpo["status"])
	}
}

func TestDepsHealthCircuitoAbiertoRetorna503Degraded(t *testing.T) {
	codigo, cuerpo := getDepsHealth(t, "open")

	if codigo != http.StatusServiceUnavailable {
		t.Fatalf("esperaba 503, obtuvo %d", codigo)
	}
	if cuerpo["status"] != "degraded" {
		t.Fatalf("esperaba status \"degraded\", obtuvo %v", cuerpo["status"])
	}
}

func TestDepsHealthHalfOpenNoEsDegradado(t *testing.T) {
	// El contrato define degraded solo con el circuito abierto: half-open es la
	// recuperación en curso, no una caída.
	codigo, cuerpo := getDepsHealth(t, "half-open")

	if codigo != http.StatusOK {
		t.Fatalf("esperaba 200 con circuito half-open, obtuvo %d", codigo)
	}
	if cuerpo["status"] != "ok" {
		t.Fatalf("esperaba status \"ok\" con circuito half-open, obtuvo %v", cuerpo["status"])
	}
}

func TestDepsHealthExponeElEstadoDeIdentityComoUpstream(t *testing.T) {
	_, cuerpo := getDepsHealth(t, "open")

	upstreams, ok := cuerpo["upstreams"].(map[string]any)
	if !ok {
		t.Fatalf("esperaba el objeto \"upstreams\", obtuvo %v", cuerpo["upstreams"])
	}
	identity, ok := upstreams["identity"].(map[string]any)
	if !ok {
		t.Fatalf("esperaba \"upstreams.identity\", obtuvo %v", upstreams)
	}
	if identity["circuit"] != "open" {
		t.Fatalf("esperaba circuit \"open\", obtuvo %v", identity["circuit"])
	}
}

func TestDepsHealthNoAfectaReady(t *testing.T) {
	// Un circuito abierto hacia identity no debe sacar a la instancia del
	// balanceo: /ready depende solo de la base propia.
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeRegisterVehicleUseCase{}, &fakeCheckVehicleUseCase{}, &fakePingable{err: nil}, &fakeCircuitReporter{estado: "open"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba /ready 200 con circuito abierto y base sana, obtuvo %d", rec.Code)
	}
}
