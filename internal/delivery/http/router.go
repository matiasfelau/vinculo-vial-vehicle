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

// NewRouter construye el motor Gin con las rutas del servicio registradas. extra
// suma las dependencias que /health/deps reporta (los consumidores de RabbitMQ).
func NewRouter(vehicles vehicleUseCase, db Pingable, extra ...DependencyCheck) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	engine.GET("/health", healthCheck)
	engine.GET("/ready", readyCheck(db))
	engine.GET("/health/deps", DepsHealthHandler(extra...))
	engine.POST("/api/v1/vehicles", RegisterVehicleHandler(vehicles))
	engine.GET("/api/v1/vehicles", CountVehiclesHandler(vehicles))
	engine.POST("/api/v1/vehicles/unlink", UnlinkVehicleHandler(vehicles))
	engine.DELETE("/api/v1/vehicles", UnlinkAllVehiclesHandler(vehicles))

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
