package lookup

import (
	"context"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

// Verificación en tiempo de compilación de que la implementación concreta de
// producción satisface la interfaz requerida por el caso de uso.
var _ PlateDecryptor = (*hpke.Decryptor)(nil)

// CheckVehicleService orquesta US-05: POST /vehicles/check.
type CheckVehicleService struct {
	repository domain.VehicleRepository
	decryptor  PlateDecryptor
	hmacSecret []byte
}

// NewCheckVehicleService construye el caso de uso con sus dependencias.
func NewCheckVehicleService(repository domain.VehicleRepository, decryptor PlateDecryptor, hmacSecret []byte) *CheckVehicleService {
	return &CheckVehicleService{repository: repository, decryptor: decryptor, hmacSecret: hmacSecret}
}

// Check desencripta patEnc y responde si la patente está vinculada en la red.
// Es, por diseño, un oráculo de pertenencia: la respuesta es estrictamente
// booleana, sin conteos ni identificadores que amplifiquen la fuga.
func (s *CheckVehicleService) Check(ctx context.Context, patEnc string) (bool, error) {
	patHashed, err := resolvePatHash(s.decryptor, s.hmacSecret, patEnc)
	if err != nil {
		return false, err
	}

	return s.repository.ExistsByPatHash(ctx, patHashed)
}
