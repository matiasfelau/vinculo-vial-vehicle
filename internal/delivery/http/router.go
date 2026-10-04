// Package http contiene los handlers HTTP expuestos por el servicio (capa de entrega).
package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Pingable define el contrato para verificar la disponibilidad de una dependencia.
type Pingable interface {
	Ping(ctx context.Context) error
}

// NewRouter construye el motor Gin con las rutas del servicio registradas.
func NewRouter(registrar registerVehicleUseCase, checker checkVehicleUseCase, db Pingable, identity circuitStateReporter) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	engine.GET("/health", healthCheck)
	engine.GET("/ready", readyCheck(db))
	engine.GET("/health/deps", DepsHealthHandler(identity))
	engine.POST("/api/v1/vehicles", RegisterVehicleHandler(registrar))
	engine.POST("/api/v1/vehicles/check", CheckVehicleHandler(checker))

	return engine
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func readyCheck(db Pingable) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "detail": "base de datos inalcanzable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
