package http

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
)

// Códigos de error estables del contrato público (US-14). La app mapea por code,
// no por status.
const (
	codeInvalidPlate          = "INVALID_PLATE"
	codeVehicleNotFound       = "VEHICLE_NOT_FOUND"
	codeDuplicateVehicle      = "DUPLICATE_VEHICLE"
	codeVehicleLimitReached   = "VEHICLE_LIMIT_REACHED"
	codeDependencyUnavailable = "DEPENDENCY_UNAVAILABLE"
	codeUnauthorized          = "UNAUTHORIZED"
	codeInternal              = "INTERNAL_ERROR"
)

// retryAfterSeconds es el valor del header Retry-After de los 503: cuánto espera
// el cliente antes de reintentar cuando la base está caída o en timeout.
const retryAfterSeconds = "5"

// vehicleUseCase es el contrato que los handlers necesitan del caso de uso, para
// poder probarlos con un doble sin depender de su implementación concreta.
type vehicleUseCase interface {
	Register(ctx context.Context, input vehicle.PlateInput) (vehicle.Summary, error)
	Unlink(ctx context.Context, input vehicle.PlateInput) (vehicle.Summary, error)
	UnlinkAll(ctx context.Context, ownerID uuid.UUID) error
	Count(ctx context.Context, ownerID uuid.UUID) (vehicle.Summary, error)
}

type plateRequest struct {
	PayloadEnc string `json:"payload_enc" binding:"required"`
}

// writeError responde con el formato {error, code} del contrato.
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": message, "code": code})
}

// ownerFromHeader lee X-User-Id (inyectado por Kong) como owner_id; nunca confía
// en un X-User-Id que no provenga del gateway. Si falta o es inválido responde 401
// y retorna false.
func ownerFromHeader(c *gin.Context, route string) (uuid.UUID, bool) {
	rawUserID := c.GetHeader("X-User-Id")
	ownerID, err := uuid.Parse(rawUserID)
	if err != nil {
		log.Printf("%s: X-User-Id ausente o inválido (%q): %v", route, rawUserID, err)
		writeError(c, http.StatusUnauthorized, codeUnauthorized, "encabezado X-User-Id ausente o inválido")
		return uuid.Nil, false
	}
	return ownerID, true
}

// plateHandler arma el handler de las dos rutas que reciben payload_enc: vincular
// y desvincular. Solo cambian el caso de uso invocado y el status de éxito.
func plateHandler(route string, status int, run func(ctx context.Context, input vehicle.PlateInput) (vehicle.Summary, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		ownerID, ok := ownerFromHeader(c, route)
		if !ok {
			return
		}

		var req plateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// El detalle del binding puede citar el cuerpo recibido: no se loguea.
			log.Printf("%s: payload mal formado", route)
			writeError(c, http.StatusBadRequest, codeInvalidPlate, "payload mal formado")
			return
		}

		summary, err := run(c.Request.Context(), vehicle.PlateInput{OwnerID: ownerID, PayloadEnc: req.PayloadEnc})
		if err != nil {
			writeVehicleError(c, route, err)
			return
		}
		c.JSON(status, summary)
	}
}

// RegisterVehicleHandler expone POST /api/v1/vehicles: vincula la patente escrita
// al usuario de la sesión. Responde 201 {count, max}.
func RegisterVehicleHandler(useCase vehicleUseCase) gin.HandlerFunc {
	return plateHandler("POST /api/v1/vehicles", http.StatusCreated, useCase.Register)
}

// UnlinkVehicleHandler expone POST /api/v1/vehicles/unlink: desvincula la patente
// escrita de entre los vehículos del usuario. Responde 200 {count, max}.
func UnlinkVehicleHandler(useCase vehicleUseCase) gin.HandlerFunc {
	return plateHandler("POST /api/v1/vehicles/unlink", http.StatusOK, useCase.Unlink)
}

// CountVehiclesHandler expone GET /api/v1/vehicles: cuántos vehículos tiene el
// usuario y el máximo. Nunca devuelve patentes.
func CountVehiclesHandler(useCase vehicleUseCase) gin.HandlerFunc {
	const route = "GET /api/v1/vehicles"
	return func(c *gin.Context) {
		ownerID, ok := ownerFromHeader(c, route)
		if !ok {
			return
		}
		summary, err := useCase.Count(c.Request.Context(), ownerID)
		if err != nil {
			writeVehicleError(c, route, err)
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}

// UnlinkAllVehiclesHandler expone DELETE /api/v1/vehicles: desvincula todos los
// vehículos del usuario. Responde 204.
func UnlinkAllVehiclesHandler(useCase vehicleUseCase) gin.HandlerFunc {
	const route = "DELETE /api/v1/vehicles"
	return func(c *gin.Context) {
		ownerID, ok := ownerFromHeader(c, route)
		if !ok {
			return
		}
		if err := useCase.UnlinkAll(c.Request.Context(), ownerID); err != nil {
			writeVehicleError(c, route, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// writeVehicleError traduce el error del caso de uso al status y code del
// contrato. El log lleva el error del caso de uso, que nunca contiene la patente.
func writeVehicleError(c *gin.Context, route string, err error) {
	log.Printf("%s: rechazado por el caso de uso: %v", route, err)

	switch {
	case errors.Is(err, domain.ErrRepositoryUnavailable):
		c.Header("Retry-After", retryAfterSeconds)
		writeError(c, http.StatusServiceUnavailable, codeDependencyUnavailable, "no se pudo consultar la base de datos")
	case errors.Is(err, domain.ErrDuplicateVehicle):
		writeError(c, http.StatusConflict, codeDuplicateVehicle, "el vehículo ya está vinculado a este propietario")
	case errors.Is(err, domain.ErrVehicleLimitReached):
		writeError(c, http.StatusConflict, codeVehicleLimitReached, "se alcanzó el máximo de vehículos vinculados")
	case errors.Is(err, domain.ErrVehicleNotFound):
		writeError(c, http.StatusNotFound, codeVehicleNotFound, "el propietario no tiene vinculada esa patente")
	case errors.Is(err, domain.ErrInvalidPlate), errors.Is(err, domain.ErrInvalidCryptogram):
		writeError(c, http.StatusBadRequest, codeInvalidPlate, "criptograma o formato de patente inválido")
	default:
		writeError(c, http.StatusInternalServerError, codeInternal, "error interno")
	}
}
