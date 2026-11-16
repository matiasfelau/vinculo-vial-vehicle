package blindindex

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// DedupKey calcula la clave de deduplicación de US-16:
// HMAC-SHA256(key, issuerID + ":" + plate), en hexadecimal (64 caracteres).
//
// Es determinística por (emisor, patente): Alert la usa para contar las alertas
// de un emisor a una misma patente sin conocer la patente. Como el emisor entra
// en el mensaje y la clave es distinta de la del Blind Index, dos emisores a la
// misma patente obtienen claves sin relación, y la clave no sirve para buscar el
// vehículo en la base de Vehicle.
//
// El mensaje se alimenta al HMAC por partes, que da el mismo resultado que
// concatenarlo, para no armar en memoria una copia de la patente que después
// habría que purgar.
func DedupKey(key []byte, issuerID string, plate []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(issuerID))
	mac.Write([]byte(":"))
	mac.Write(plate)
	return hex.EncodeToString(mac.Sum(nil))
}
