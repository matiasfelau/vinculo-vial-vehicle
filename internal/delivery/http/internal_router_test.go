package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

const secretoDePrueba = "un-secreto-de-servicio-bien-largo"

func TestInternalHealthzSinTokenDevuelve401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba)

	req := httptest.NewRequest(http.MethodGet, "/internal/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
}

func TestInternalHealthzConTokenIncorrectoDevuelve401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba)

	req := httptest.NewRequest(http.MethodGet, "/internal/healthz", nil)
	req.Header.Set(HeaderInternalServiceToken, "esto-no-es-el-secreto")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
}

func TestInternalHealthzConTokenCorrectoDevuelve200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba)

	req := httptest.NewRequest(http.MethodGet, "/internal/healthz", nil)
	req.Header.Set(HeaderInternalServiceToken, secretoDePrueba)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
}

// El status nunca debe delatar si una ruta interna existe: sin el secreto
// correcto, una ruta real y una inventada tienen que responder exactamente
// lo mismo.
func TestInternalRouterDevuelve401AntesDeRevelarSiLaRutaExiste(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba)

	req := httptest.NewRequest(http.MethodPost, "/internal/una-ruta-que-no-existe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
}

// El router interno y el público son dos *gin.Engine independientes: ninguna
// ruta del contrato público (/api/v1/vehicles) debería resolver acá, ni
// siquiera con el secreto de servicio correcto.
func TestInternalRouterNoExponeLasRutasPublicas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewInternalRouter(secretoDePrueba)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vehicles", nil)
	req.Header.Set(HeaderInternalServiceToken, secretoDePrueba)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d", rec.Code)
	}
}
