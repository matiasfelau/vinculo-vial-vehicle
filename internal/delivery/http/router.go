// Package http contiene los handlers HTTP expuestos por el servicio (capa de entrega).
package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter construye el motor Gin con las rutas del servicio registradas.
func NewRouter(registrar registerVehicleUseCase, checker checkVehicleUseCase) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	engine.GET("/health", healthCheck)
	engine.POST("/api/v1/vehicles", RegisterVehicleHandler(registrar))
	engine.POST("/api/v1/vehicles/check", CheckVehicleHandler(checker))

	return engine
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
