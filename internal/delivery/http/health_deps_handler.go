package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// circuitStateReporter es el contrato que el handler de /health/deps necesita
// del circuit breaker de una dependencia saliente, para poder probarlo con un
// doble sin depender de su implementación concreta.
type circuitStateReporter interface {
	CircuitState() string
}

// circuitOpen es el estado del breaker que marca la dependencia como degradada.
const circuitOpen = "open"

// DepsHealthHandler expone GET /health/deps. Reporta el estado del circuit
// breaker hacia identity: 200 con status "ok" si el circuito no está abierto,
// y 503 con status "degraded" si lo está. No realiza llamadas de red y no
// interviene en /ready: el balanceo de Kong depende solo de la base propia.
func DepsHealthHandler(identity circuitStateReporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		estado := identity.CircuitState()

		upstreams := gin.H{"identity": gin.H{"circuit": estado}}
		if estado == circuitOpen {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":    "degraded",
				"error":     "identity no disponible: circuito abierto",
				"upstreams": upstreams,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "upstreams": upstreams})
	}
}
