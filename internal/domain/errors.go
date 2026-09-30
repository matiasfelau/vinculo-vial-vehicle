package domain

import "errors"

var (
	// ErrDuplicateVehicle indica que el par (owner_id, pat_hashed) ya existe.
	ErrDuplicateVehicle = errors.New("domain: el vehículo ya está vinculado a este propietario")

	// ErrOwnershipMismatch indica que Identity confirmó que el DNI no corresponde al propietario.
	ErrOwnershipMismatch = errors.New("domain: el dni no corresponde al propietario de la sesión")

	// ErrOwnershipUnavailable indica que Identity es inalcanzable: timeout o error de
	// conexión. El destino nunca respondió, así que se mapea a 503 y cuenta como falla
	// para el circuit breaker de esa dependencia.
	ErrOwnershipUnavailable = errors.New("domain: no se pudo verificar la propiedad del vehículo")

	// ErrOwnershipUpstreamFailure indica que Identity respondió, pero con un error de
	// servidor (5xx) o un cuerpo que no pudo interpretarse. A diferencia de
	// ErrOwnershipUnavailable, el destino sí fue alcanzado; se mapea a 502 y también
	// cuenta como falla para el circuit breaker.
	ErrOwnershipUpstreamFailure = errors.New("domain: identity respondió con un error de servidor")

	// ErrRepositoryUnavailable indica que la base de datos propia es inalcanzable o que
	// una consulta excedió su plazo (context.DeadlineExceeded). Se mapea a 503: es una
	// indisponibilidad de infraestructura, no un defecto del servicio.
	ErrRepositoryUnavailable = errors.New("domain: no se pudo consultar la base de datos")

	// ErrInvalidPlate indica que la patente desencriptada no cumple el formato argentino esperado.
	ErrInvalidPlate = errors.New("domain: formato de patente inválido")

	// ErrInvalidCryptogram indica que payload_enc no pudo desencriptarse o no tiene el formato esperado.
	ErrInvalidCryptogram = errors.New("domain: criptograma de patente inválido")

	// ErrOwnerLookupUnavailable indica que no pudo consultarse la titularidad contra el DNRPA.
	ErrOwnerLookupUnavailable = errors.New("domain: no se pudo consultar la titularidad del vehículo")
)
