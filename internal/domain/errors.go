package domain

import "errors"

var (
	// ErrDuplicateVehicle indica que el par (owner_id, pat_hashed) ya existe.
	ErrDuplicateVehicle = errors.New("domain: el vehículo ya está vinculado a este propietario")

	// ErrVehicleLimitReached indica que el propietario ya tiene el máximo de
	// vehículos vinculados (VEHICLE_MAX_PER_USER).
	ErrVehicleLimitReached = errors.New("domain: el propietario alcanzó el máximo de vehículos vinculados")

	// ErrVehicleNotFound indica que el propietario no tiene vinculada la patente
	// que se quiso desvincular. Que otro titular la tenga vinculada no cuenta.
	ErrVehicleNotFound = errors.New("domain: el propietario no tiene vinculada esa patente")

	// ErrRepositoryUnavailable indica que la base de datos propia es inalcanzable o que
	// una consulta excedió su plazo (context.DeadlineExceeded). Se mapea a 503: es una
	// indisponibilidad de infraestructura, no un defecto del servicio.
	ErrRepositoryUnavailable = errors.New("domain: no se pudo consultar la base de datos")

	// ErrInvalidPlate indica que la patente desencriptada no cumple el formato argentino esperado.
	ErrInvalidPlate = errors.New("domain: formato de patente inválido")

	// ErrInvalidCryptogram indica que payload_enc no pudo desencriptarse o no tiene el formato esperado.
	ErrInvalidCryptogram = errors.New("domain: criptograma de patente inválido")
)
