package domain

import "context"

// DNRPAClient define la consulta de titularidad contra el registro automotor.
// Dada una patente, retorna el DNI del titular legal en claro; el llamador es
// responsable de purgarlo (secure.Wipe) inmediatamente después de usarlo.
type DNRPAClient interface {
	GetOwnerDNI(ctx context.Context, plate []byte) ([]byte, error)
}
