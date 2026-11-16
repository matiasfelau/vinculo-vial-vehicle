package http

import (
	"crypto/subtle"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HeaderInternalServiceToken es la cabecera donde el llamador (vinculo-vial-alert)
// debe enviar el secreto compartido de servicio a servicio en cada petición a
// /internal/*. No es un token de sesión de usuario ni lo emite este servicio: es
// un valor estático, provisto por configuración en ambos extremos.
const HeaderInternalServiceToken = "X-Internal-Service-Token"

// internalUseCase agrupa los casos de uso que expone el router interno.
type internalUseCase interface {
	resolveOwnersUseCase
	dedupKeyUseCase
}

// NewInternalRouter construye el motor Gin de las rutas /internal/*.
//
// Se sirve por separado de NewRouter, en su propio *http.Server y su propio
// puerto (ver cmd/api): ni Kong ni el docker-compose de este servicio lo
// publican fuera de la red interna. Esa separación de puerto es una condición
// necesaria pero no suficiente para el aislamiento —dentro de la red Docker
// cualquier contenedor alcanza cualquier puerto de otro—, así que
// requireServiceToken exige además, en toda petición, el secreto de servicio
// como segunda capa de la defensa en profundidad.
func NewInternalRouter(serviceToken string, resolver internalUseCase) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())
	engine.Use(logInternalRequest())
	engine.Use(requireServiceToken(serviceToken))

	engine.GET("/internal/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	engine.POST("/internal/resolve-owner", ResolveOwnerHandler(resolver))
	engine.POST("/internal/dedup-key", DedupKeyHandler(resolver))

	return engine
}

// logInternalRequest registra la llegada de toda petición al router interno,
// antes de cualquier otra validación (incluida requireServiceToken). Permite
// distinguir, ante un reclamo de un consumidor (p. ej. Alert) de que sus
// peticiones "mueren en el camino", si la petición efectivamente llegó a
// este proceso (problema de aplicación o de token) o si nunca llegó
// (problema de red, DNS o binding de puerto): esto último no deja rastro acá.
func logInternalRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Printf("internal: petición recibida %s %s desde %s", c.Request.Method, c.Request.URL.Path, c.Request.RemoteAddr)
		c.Next()
	}
}

// requireServiceToken exige, en toda petición, el secreto de servicio a
// servicio configurado. Al aplicarse antes de que el router resuelva si la
// ruta existe, una petición sin el secreto correcto recibe 401 tanto si el
// path es real como si no: el status nunca delata qué rutas expone este
// servidor.
//
// La comparación es a tiempo constante: el largo del valor recibido no es
// secreto en sí mismo, pero el contenido nunca se compara con el operador
// `==`, que corta en el primer byte distinto.
func requireServiceToken(token string) gin.HandlerFunc {
	want := []byte(token)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader(HeaderInternalServiceToken))

		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no autorizado"})
			return
		}

		c.Next()
	}
}
