package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba status %d, obtuvo %d", http.StatusOK, rec.Code)
	}
}

func TestReadyCheckOk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba status %d, obtuvo %d", http.StatusOK, rec.Code)
	}
}

func TestReadyCheckError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{err: context.DeadlineExceeded})

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperaba status %d, obtuvo %d", http.StatusServiceUnavailable, rec.Code)
	}
}

type fakePingable struct {
	err error
}

func (f *fakePingable) Ping(ctx context.Context) error {
	return f.err
}

// El check público de existencia de patentes se eliminó (US-08): es un oráculo que
// ahora resuelve Alert a través de /internal/resolve-owner.
func TestRouterNoExponeElCheckPublicoDePatentes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeVehicleUseCase{}, &fakePingable{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vehicles/check", strings.NewReader(`{"pat_enc":"criptograma"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d", rec.Code)
	}
}
