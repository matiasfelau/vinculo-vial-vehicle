package domain

import "errors"

var (
	// ErrDuplicateVehicle indica que el par (owner_id, pat_hashed) ya existe.
	ErrDuplicateVehicle = errors.New("domain: el vehículo ya está vinculado a este propietario")

	// ErrOwnershipMismatch indica que Identity confirmó que el DNI no corresponde al propietario.
	ErrOwnershipMismatch = errors.New("domain: el dni no corresponde al propietario de la sesión")

	// ErrOwnershipUnavailable indica que no pudo verificarse la propiedad (Identity caído o timeout).
	ErrOwnershipUnavailable = errors.New("domain: no se pudo verificar la propiedad del vehículo")

	// ErrInvalidPlate indica que la patente desencriptada no cumple el formato argentino esperado.
	ErrInvalidPlate = errors.New("domain: formato de patente inválido")
)
