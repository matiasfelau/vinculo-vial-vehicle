package http

import (
	"net/http"
	"strings"

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

const (
	depUp   = "up"
	depDown = "down"
)

// DependencyCheck es una dependencia adicional que /health/deps reporta bajo su
// nombre, por ejemplo un consumidor de RabbitMQ.
type DependencyCheck struct {
	Name    string
	Healthy func() bool
}

// DepsHealthHandler expone GET /health/deps. Reporta el estado del circuit
// breaker hacia identity y el de cada dependencia adicional (los consumidores de
// RabbitMQ): 200 con status "ok" si el circuito no está abierto y todas están
// arriba, y 503 con status "degraded" si no. No realiza llamadas de red y no
// interviene en /ready: el balanceo de Kong depende solo de la base propia.
func DepsHealthHandler(identity circuitStateReporter, extra ...DependencyCheck) gin.HandlerFunc {
	return func(c *gin.Context) {
		estado := identity.CircuitState()
		body := gin.H{"status": "ok", "upstreams": gin.H{"identity": gin.H{"circuit": estado}}}

		var caidas []string
		if estado == circuitOpen {
			caidas = append(caidas, "identity no disponible: circuito abierto")
		}

		if len(extra) > 0 {
			deps := gin.H{}
			for _, check := range extra {
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
