package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	depUp   = "up"
	depDown = "down"
)

// DependencyCheck es una dependencia que /health/deps reporta bajo su nombre,
// por ejemplo un consumidor de RabbitMQ.
type DependencyCheck struct {
	Name    string
	Healthy func() bool
}

// DepsHealthHandler expone GET /health/deps. Reporta el estado de cada
// dependencia (los consumidores de RabbitMQ): 200 con status "ok" si todas están
// arriba, y 503 con status "degraded" si alguna no. No realiza llamadas de red y
// no interviene en /ready: el balanceo de Kong depende solo de la base propia.
func DepsHealthHandler(checks ...DependencyCheck) gin.HandlerFunc {
	return func(c *gin.Context) {
		body := gin.H{"status": "ok"}

		var caidas []string
		if len(checks) > 0 {
			deps := gin.H{}
			for _, check := range checks {
				if check.Healthy() {
					deps[check.Name] = depUp
					continue
				}
				deps[check.Name] = depDown
				caidas = append(caidas, check.Name+" no disponible")
			}
			body["deps"] = deps
		}

		if len(caidas) > 0 {
			body["status"] = "degraded"
			body["error"] = strings.Join(caidas, "; ")
			c.JSON(http.StatusServiceUnavailable, body)
			return
		}

		c.JSON(http.StatusOK, body)
	}
}
