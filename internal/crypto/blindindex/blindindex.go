// Package blindindex calcula el Blind Index determinístico de la patente,
// usado como índice de búsqueda/unicidad sin exponer el valor en claro.
package blindindex

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Compute calcula el HMAC-SHA256 de la patente normalizada con el secreto configurado
// y lo retorna en representación hexadecimal, apta para persistir e indexar.
func Compute(secret []byte, plate []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(plate)
	return hex.EncodeToString(mac.Sum(nil))
}
