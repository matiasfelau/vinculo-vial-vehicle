package vehicle

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

const (
	testSecret = "secreto"
	testMax    = 3
)

// fakeRepository registra lo que el servicio le pide y devuelve lo configurado.
type fakeRepository struct {
	createErr   error
	createCount int
	created     *domain.Vehicle
	createMax   int
	createCalls int

	countValue int
	countErr   error
	countOwner uuid.UUID

	unlinkRemaining int
	unlinkErr       error
	unlinkCalls     int
	unlinkOwner     uuid.UUID
	unlinkPlate     string

	deleteAllErr   error
	deleteAllCalls int
	deleteAllOwner uuid.UUID
}

func (f *fakeRepository) Create(ctx context.Context, vehicle *domain.Vehicle, maxPerOwner int) (int, error) {
	f.createCalls++
	f.created = vehicle
	f.createMax = maxPerOwner
	return f.createCount, f.createErr
}

func (f *fakeRepository) CountByOwner(ctx context.Context, ownerID uuid.UUID) (int, error) {
	f.countOwner = ownerID
	return f.countValue, f.countErr
}

func (f *fakeRepository) DeleteByOwnerAndPlate(ctx context.Context, ownerID uuid.UUID, patHashed string) (int, error) {
	f.unlinkCalls++
	f.unlinkOwner = ownerID
	f.unlinkPlate = patHashed
	return f.unlinkRemaining, f.unlinkErr
}

func (f *fakeRepository) DeleteByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	f.deleteAllCalls++
	f.deleteAllOwner = ownerID
	return 0, f.deleteAllErr
}

func (f *fakeRepository) FindOwnersOrReservePending(ctx context.Context, patHashed string, proposal domain.PendingProposal) ([]uuid.UUID, *uuid.UUID, error) {
	return nil, nil, nil
}

func (f *fakeRepository) DeleteExpiredPending(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}

type fakePayloadDecryptor struct {
	payload []byte
	err     error
}

func (f *fakePayloadDecryptor) Decrypt(ciphertextBase64 string) ([]byte, error) {
	return f.payload, f.err
}

// fakePlateCipher conserva el buffer que recibió para poder comprobar después que
// el servicio lo purgó.
type fakePlateCipher struct {
	ciphertext string
	err        error
	received   []byte
	snapshot   string
}

func (f *fakePlateCipher) Encrypt(plaintext []byte) (string, error) {
	f.received = plaintext
	f.snapshot = string(plaintext)
	return f.ciphertext, f.err
}

// payloadConCampos serializa un payload con los campos dados: los []byte viajan
// en base64, igual que el payload real.
func payloadConCampos(t *testing.T, campos map[string]any) []byte {
	t.Helper()

	raw, err := json.Marshal(campos)
	if err != nil {
		t.Fatalf("no se pudo serializar el payload de prueba: %v", err)
	}
	return raw
}

func payloadDePatente(t *testing.T, patente string) []byte {
	t.Helper()
	return payloadConCampos(t, map[string]any{"patente": []byte(patente)})
}

func nuevoServicio(repo *fakeRepository, payload []byte) (*Service, *fakePlateCipher) {
	cipher := &fakePlateCipher{ciphertext: "cifrado"}
	return NewService(repo, &fakePayloadDecryptor{payload: payload}, cipher, []byte(testSecret), testMax), cipher
}

func entrada(ownerID uuid.UUID) PlateInput {
	return PlateInput{OwnerID: ownerID, PayloadEnc: "criptograma-payload"}
}

func TestRegisterVinculaLaPatenteAlUsuarioYRetornaElConteo(t *testing.T) {
	repo := &fakeRepository{createCount: 2}
	service, _ := nuevoServicio(repo, payloadDePatente(t, "AB123CD"))
	ownerID := uuid.New()

	summary, err := service.Register(context.Background(), entrada(ownerID))

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if summary != (Summary{Count: 2, Max: testMax}) {
		t.Fatalf("summary = %+v, esperaba {2 3}", summary)
	}
	if repo.createCalls != 1 || repo.created.OwnerID != ownerID {
		t.Fatalf("esperaba un alta del usuario %s, hubo %d (%+v)", ownerID, repo.createCalls, repo.created)
	}
	if repo.createMax != testMax {
		t.Fatalf("el tope enviado al repositorio fue %d, esperaba %d", repo.createMax, testMax)
	}
	if repo.created.PatAES != "cifrado" {
		t.Fatalf("pat_aes = %q, esperaba el criptograma de la patente", repo.created.PatAES)
	}
	if want := blindindex.Compute([]byte(testSecret), []byte("AB123CD")); repo.created.PatHashed != want {
		t.Fatalf("pat_hashed no es el blind index de la patente canónica")
	}
}

func TestRegisterIgnoraElChasisQueEnviaLaAppAnterior(t *testing.T) {
	conChasis := payloadConCampos(t, map[string]any{"patente": []byte("AB123CD"), "chasis": []byte("1HGCM82633A004352")})
	// Un chasis que ni siquiera es base64 válido tampoco puede romper el alta.
	chasisRoto := []byte(`{"patente":"QUIxMjNDRA==","chasis":"%%no-es-base64%%"}`)

	for nombre, payload := range map[string][]byte{"con chasis": conChasis, "con chasis ilegible": chasisRoto} {
		t.Run(nombre, func(t *testing.T) {
			repo := &fakeRepository{createCount: 1}
			service, _ := nuevoServicio(repo, payload)

			if _, err := service.Register(context.Background(), entrada(uuid.New())); err != nil {
				t.Fatalf("no se esperaba error: %v", err)
			}
			if want := blindindex.Compute([]byte(testSecret), []byte("AB123CD")); repo.created.PatHashed != want {
				t.Fatal("el chasis alteró la patente vinculada")
			}
		})
	}
}

func TestRegisterSinChasisVinculaIgual(t *testing.T) {
	repo := &fakeRepository{createCount: 1}
	service, _ := nuevoServicio(repo, payloadDePatente(t, "AAA999"))

	if _, err := service.Register(context.Background(), entrada(uuid.New())); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("esperaba una alta, hubo %d", repo.createCalls)
	}
}

func TestRegisterFormatosValidosSeNormalizanAntesDelBlindIndex(t *testing.T) {
	casos := map[string]string{
		"AAA999":     "AAA999",  // anterior a 2016
		"AA999AA":    "AA999AA", // Mercosur
		"ab123cd":    "AB123CD",
		" AB-123 CD": "AB123CD",
		"abc 123":    "ABC123",
	}

	for escrita, canonica := range casos {
		t.Run(escrita, func(t *testing.T) {
			repo := &fakeRepository{createCount: 1}
			service, _ := nuevoServicio(repo, payloadDePatente(t, escrita))

			if _, err := service.Register(context.Background(), entrada(uuid.New())); err != nil {
				t.Fatalf("la patente %q debía ser válida: %v", escrita, err)
			}
			if want := blindindex.Compute([]byte(testSecret), []byte(canonica)); repo.created.PatHashed != want {
				t.Fatalf("la patente %q no se normalizó a %q", escrita, canonica)
			}
		})
	}
}

func TestRegisterFormatosInvalidosRetornanErrInvalidPlateSinPersistir(t *testing.T) {
	for _, patente := range []string{"", "AB12CD3", "1234567", "ABCD123", "AAA9999", "AB123C", "AB 1234 CD", "ÑB123CD"} {
		t.Run(patente, func(t *testing.T) {
			repo := &fakeRepository{}
			service, _ := nuevoServicio(repo, payloadDePatente(t, patente))

			_, err := service.Register(context.Background(), entrada(uuid.New()))

			if !errors.Is(err, domain.ErrInvalidPlate) {
				t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
			}
			if repo.createCalls != 0 {
				t.Fatal("no debía persistirse nada")
			}
		})
	}
}

func TestRegisterPayloadSinPatenteRetornaErrInvalidPlate(t *testing.T) {
	repo := &fakeRepository{}
	service, _ := nuevoServicio(repo, payloadConCampos(t, map[string]any{"chasis": []byte("1HGCM82633A004352")}))

	_, err := service.Register(context.Background(), entrada(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}
}

func TestRegisterCriptogramaInvalidoRetornaErrInvalidCryptogram(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakePayloadDecryptor{err: errors.New("criptograma corrupto")}, &fakePlateCipher{}, []byte(testSecret), testMax)

	_, err := service.Register(context.Background(), entrada(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterPayloadConJSONMalformadoRetornaErrInvalidCryptogramSinCitarElPayload(t *testing.T) {
	repo := &fakeRepository{}
	service, _ := nuevoServicio(repo, []byte("ZZZ999 no es json"))

	_, err := service.Register(context.Background(), entrada(uuid.New()))

	if !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}
	if strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "ZZZ") {
		t.Fatalf("el error cita el contenido del payload: %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("no debía persistirse nada")
	}
}

func TestRegisterPropagaLosErroresDelRepositorio(t *testing.T) {
	casos := []error{domain.ErrDuplicateVehicle, domain.ErrVehicleLimitReached, domain.ErrRepositoryUnavailable}

	for _, esperado := range casos {
		t.Run(esperado.Error(), func(t *testing.T) {
			repo := &fakeRepository{createErr: esperado}
			service, _ := nuevoServicio(repo, payloadDePatente(t, "AB123CD"))

			summary, err := service.Register(context.Background(), entrada(uuid.New()))

			if !errors.Is(err, esperado) {
				t.Fatalf("esperaba %v, obtuvo %v", esperado, err)
			}
			if summary != (Summary{}) {
				t.Fatalf("un alta fallida no debe devolver conteo, obtuvo %+v", summary)
			}
		})
	}
}

func TestRegisterFalloDeCifradoNoPersiste(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakePayloadDecryptor{payload: payloadDePatente(t, "AB123CD")}, &fakePlateCipher{err: errors.New("fallo de cifrado")}, []byte(testSecret), testMax)

	_, err := service.Register(context.Background(), entrada(uuid.New()))

	if err == nil {
		t.Fatal("esperaba error al no poder cifrar la patente")
	}
	if repo.createCalls != 0 {
		t.Fatal("no debía persistirse nada")
	}
	if strings.Contains(err.Error(), "AB123CD") {
		t.Fatalf("el error contiene la patente: %v", err)
	}
}

func assertPurgado(t *testing.T, nombre string, buf []byte) {
	t.Helper()
	if len(buf) == 0 {
		t.Fatalf("%s: el buffer de la prueba está vacío", nombre)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("%s: el byte %d no fue purgado: %d", nombre, i, b)
		}
	}
}

func TestRegisterPurgaLosBuffersConLaPatenteEnExitoYEnError(t *testing.T) {
	casos := map[string]error{"exito": nil, "error de persistencia": errors.New("fallo de base de datos")}

	for nombre, createErr := range casos {
		t.Run(nombre, func(t *testing.T) {
			payload := payloadDePatente(t, "AB123CD")
			repo := &fakeRepository{createErr: createErr}
			service, cipher := nuevoServicio(repo, payload)

			_, _ = service.Register(context.Background(), entrada(uuid.New()))

			assertPurgado(t, "payload desencriptado", payload)
			if cipher.snapshot != "AB123CD" {
				t.Fatalf("el cifrador debía recibir la patente canónica, recibió %q", cipher.snapshot)
			}
			assertPurgado(t, "patente normalizada", cipher.received)
		})
	}
}

func TestRegisterPurgaElPayloadAnteUnaPatenteInvalida(t *testing.T) {
	payload := payloadDePatente(t, "NO-ES-PATENTE")
	service, _ := nuevoServicio(&fakeRepository{}, payload)

	_, _ = service.Register(context.Background(), entrada(uuid.New()))

	assertPurgado(t, "payload desencriptado", payload)
}

func TestUnlinkBuscaPorBlindIndexEntreLosVehiculosDelUsuario(t *testing.T) {
	repo := &fakeRepository{unlinkRemaining: 1}
	service, _ := nuevoServicio(repo, payloadDePatente(t, "ab-123-cd"))
	ownerID := uuid.New()

	summary, err := service.Unlink(context.Background(), entrada(ownerID))

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if summary != (Summary{Count: 1, Max: testMax}) {
		t.Fatalf("summary = %+v, esperaba {1 3}", summary)
	}
	if repo.unlinkOwner != ownerID {
		t.Fatalf("se desvinculó del usuario %s, esperaba %s", repo.unlinkOwner, ownerID)
	}
	if want := blindindex.Compute([]byte(testSecret), []byte("AB123CD")); repo.unlinkPlate != want {
		t.Fatal("la desvinculación no buscó por el blind index de la patente canónica")
	}
}

func TestUnlinkPropagaNoEncontradaYIndisponibilidad(t *testing.T) {
	for _, esperado := range []error{domain.ErrVehicleNotFound, domain.ErrRepositoryUnavailable} {
		t.Run(esperado.Error(), func(t *testing.T) {
			repo := &fakeRepository{unlinkErr: esperado}
			service, _ := nuevoServicio(repo, payloadDePatente(t, "AB123CD"))

			summary, err := service.Unlink(context.Background(), entrada(uuid.New()))

			if !errors.Is(err, esperado) {
				t.Fatalf("esperaba %v, obtuvo %v", esperado, err)
			}
			if summary != (Summary{}) {
				t.Fatalf("una desvinculación fallida no debe devolver conteo, obtuvo %+v", summary)
			}
		})
	}
}

func TestUnlinkConPatenteOCriptogramaInvalidosNoTocaLaBD(t *testing.T) {
	repo := &fakeRepository{}

	servicioInvalido, _ := nuevoServicio(repo, payloadDePatente(t, "XX"))
	if _, err := servicioInvalido.Unlink(context.Background(), entrada(uuid.New())); !errors.Is(err, domain.ErrInvalidPlate) {
		t.Fatalf("esperaba domain.ErrInvalidPlate, obtuvo %v", err)
	}

	servicioCorrupto := NewService(repo, &fakePayloadDecryptor{err: errors.New("corrupto")}, &fakePlateCipher{}, []byte(testSecret), testMax)
	if _, err := servicioCorrupto.Unlink(context.Background(), entrada(uuid.New())); !errors.Is(err, domain.ErrInvalidCryptogram) {
		t.Fatalf("esperaba domain.ErrInvalidCryptogram, obtuvo %v", err)
	}

	if repo.unlinkCalls != 0 {
		t.Fatal("no debía tocarse la base con una patente o un criptograma inválidos")
	}
}

func TestUnlinkPurgaElPayload(t *testing.T) {
	payload := payloadDePatente(t, "AB123CD")
	service, _ := nuevoServicio(&fakeRepository{}, payload)

	_, _ = service.Unlink(context.Background(), entrada(uuid.New()))

	assertPurgado(t, "payload desencriptado", payload)
}

func TestUnlinkAllDesvinculaSoloAlUsuarioDeLaSesion(t *testing.T) {
	repo := &fakeRepository{}
	service, _ := nuevoServicio(repo, nil)
	ownerID := uuid.New()

	if err := service.UnlinkAll(context.Background(), ownerID); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if repo.deleteAllCalls != 1 || repo.deleteAllOwner != ownerID {
		t.Fatalf("esperaba un borrado de los vehículos de %s, hubo %d de %s", ownerID, repo.deleteAllCalls, repo.deleteAllOwner)
	}
}

func TestUnlinkAllPropagaElErrorDelRepositorio(t *testing.T) {
	service, _ := nuevoServicio(&fakeRepository{deleteAllErr: domain.ErrRepositoryUnavailable}, nil)

	if err := service.UnlinkAll(context.Background(), uuid.New()); !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestCountRetornaElConteoDelUsuarioYElMaximo(t *testing.T) {
	repo := &fakeRepository{countValue: 2}
	service, _ := nuevoServicio(repo, nil)
	ownerID := uuid.New()

	summary, err := service.Count(context.Background(), ownerID)

	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if summary != (Summary{Count: 2, Max: testMax}) {
		t.Fatalf("summary = %+v, esperaba {2 3}", summary)
	}
	if repo.countOwner != ownerID {
		t.Fatalf("se contó para %s, esperaba %s", repo.countOwner, ownerID)
	}
}

func TestCountPropagaElErrorDelRepositorio(t *testing.T) {
	service, _ := nuevoServicio(&fakeRepository{countErr: domain.ErrRepositoryUnavailable}, nil)

	if _, err := service.Count(context.Background(), uuid.New()); !errors.Is(err, domain.ErrRepositoryUnavailable) {
		t.Fatalf("esperaba domain.ErrRepositoryUnavailable, obtuvo %v", err)
	}
}

func TestSummarySerializaSoloCountYMax(t *testing.T) {
	raw, err := json.Marshal(Summary{Count: 1, Max: 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"count":1,"max":3}` {
		t.Fatalf("cuerpo = %s", raw)
	}
}
