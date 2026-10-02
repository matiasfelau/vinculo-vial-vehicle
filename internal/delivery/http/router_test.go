package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeRegisterVehicleUseCase{}, &fakeCheckVehicleUseCase{}, &fakePingable{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba status %d, obtuvo %d", http.StatusOK, rec.Code)
	}
}

func TestReadyCheckOk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeRegisterVehicleUseCase{}, &fakeCheckVehicleUseCase{}, &fakePingable{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba status %d, obtuvo %d", http.StatusOK, rec.Code)
	}
}

func TestReadyCheckError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeRegisterVehicleUseCase{}, &fakeCheckVehicleUseCase{}, &fakePingable{err: context.DeadlineExceeded})

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
