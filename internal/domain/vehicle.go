// Package domain contiene las entidades y contratos del negocio, sin dependencias
// de frameworks HTTP, ORM ni de librerías de criptografía concretas.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Vehicle representa la vinculación entre un propietario y una patente.
// La patente nunca se conserva en claro: PatHashed es el Blind Index (HMAC-SHA256)
// usado para la restricción de unicidad y PatAES es el valor cifrado at-rest (AES-256-GCM).
type Vehicle struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	PatHashed string
	PatAES    string
	CreatedAt time.Time
}
