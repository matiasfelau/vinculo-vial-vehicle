package domain

import "github.com/google/uuid"

// OwnerResolution es el resultado de resolver los dueños de una patente para
// una alerta. PatEncIA va siempre. PendingRef solo existe cuando la patente no
// tiene dueños: referencia opaca de la reserva pendiente, que no identifica
// al vehículo fuera de la ventana de entrega.
type OwnerResolution struct {
	OwnerIDs   []uuid.UUID
	PatEncIA   string
	PendingRef *uuid.UUID
}
