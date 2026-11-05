package domain

import "github.com/google/uuid"

// OwnerResolution es el resultado de resolver los dueños de una patente para
// una alerta. PatEncIA, PatEncAudit y AuditKeyID van siempre. PendingRef solo
// existe cuando la patente no tiene dueños: referencia opaca de la reserva
// pendiente, que no identifica al vehículo fuera de la ventana de entrega.
//
// PatEncAudit es la patente canónica sellada hacia Audit (base64 de enc‖ct) y
// AuditKeyID identifica la clave pública con la que se selló, para que Audit
// sepa qué clave privada abre el registro tras una rotación.
type OwnerResolution struct {
	OwnerIDs    []uuid.UUID
	PatEncIA    string
	PatEncAudit string
	AuditKeyID  string
	PendingRef  *uuid.UUID
}
