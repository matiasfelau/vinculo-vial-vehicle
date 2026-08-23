// Package dnrpa implementa la consulta de titularidad contra el registro automotor.
package dnrpa

import "context"

// MockDNRPAAdapter simula la consulta al DNRPA: para cualquier patente, retorna el
// DNI configurado en MOCK_DNRPA_OWNER_DNI. Existe únicamente para habilitar pruebas
// end-to-end sin depender de una integración real con el registro automotor.
type MockDNRPAAdapter struct {
	ownerDNI []byte
}

// NewMockDNRPAAdapter construye el adaptador con el DNI fijo a retornar.
func NewMockDNRPAAdapter(ownerDNI []byte) *MockDNRPAAdapter {
	return &MockDNRPAAdapter{ownerDNI: ownerDNI}
}

// GetOwnerDNI ignora la patente recibida y retorna siempre el DNI configurado.
// Se retorna una copia nueva en cada llamada: el llamador purga (Wipe) el buffer
// que recibe, y hacerlo sobre el slice interno del adaptador lo dejaría inutilizable
// para consultas posteriores.
func (a *MockDNRPAAdapter) GetOwnerDNI(ctx context.Context, plate []byte) ([]byte, error) {
	dni := make([]byte, len(a.ownerDNI))
	copy(dni, a.ownerDNI)
	return dni, nil
}
