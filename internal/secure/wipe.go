// Package secure provee utilidades de Zero-Retention para datos sensibles en memoria.
package secure

// Wipe sobrescribe con ceros cada byte del slice recibido.
//
// El dato sensible en claro (patente, DNI) debe manejarse exclusivamente como []byte
// de punta a punta: un string de Go es inmutable y el runtime puede copiarlo o
// reubicarlo en memoria, por lo que sobrescribirlo no tiene efecto real. Wipe solo
// tiene sentido sobre []byte.
func Wipe(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
