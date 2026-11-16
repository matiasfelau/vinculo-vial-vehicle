package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/blindindex"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/symmetric"
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/vehicle"
	"gorm.io/gorm"
)

// apiVinculacion levanta la API pública real de US-14 (router Gin, caso de uso,
// HPKE, AES y PostgreSQL) y actúa de app: cifra cada payload con la clave pública
// de Vehicle, igual que el cliente.
type apiVinculacion struct {
	t       *testing.T
	router  http.Handler
	repo    *postgres.VehicleRepository
	db      *gorm.DB
	encrypt *hpke.Encryptor
	owners  []uuid.UUID
	bodies  []string
}

const topeE2E = 3

func nuevaAPI(t *testing.T) *apiVinculacion {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL no configurada: se omite la prueba de punta a punta contra PostgreSQL")
	}

	db, err := postgres.Connect(dsn)
	if err != nil {
		t.Fatalf("no se pudo conectar a la base de datos de prueba: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := postgres.AutoMigrate(db); err != nil {
		t.Fatalf("no se pudo migrar el esquema: %v", err)
	}

	publicKey, privateKey := newKeyPair(t)
	rawPrivate, err := privateKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave privada: %v", err)
	}
	decryptor, err := hpke.NewDecryptor(base64.StdEncoding.EncodeToString(rawPrivate))
	if err != nil {
		t.Fatalf("no se pudo crear el desencriptador: %v", err)
	}
	encryptor, err := hpke.NewEncryptor(publicKey)
	if err != nil {
		t.Fatalf("no se pudo crear el cifrador de la app: %v", err)
	}
	cipher, err := symmetric.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatalf("no se pudo crear el cifrado at-rest: %v", err)
	}

	repo := postgres.NewVehicleRepository(db)
	service := vehicle.NewService(repo, decryptor, cipher, []byte("secreto-e2e"), topeE2E)

	gin.SetMode(gin.TestMode)
	api := &apiVinculacion{t: t, router: httpdelivery.NewRouter(service, repo), repo: repo, db: db, encrypt: encryptor}

	t.Cleanup(func() {
		for _, owner := range api.owners {
			db.Exec("DELETE FROM vehicles WHERE owner_id = ?", owner)
			db.Exec("DELETE FROM outbox_events WHERE actor = ?", owner)
		}
	})
	return api
}

func (a *apiVinculacion) usuario() uuid.UUID {
	id := uuid.New()
	a.owners = append(a.owners, id)
	return id
}

// patenteAlAzar devuelve una patente AAA999 que ninguna otra prueba usa.
func patenteAlAzar(t *testing.T) string {
	t.Helper()

	const letras = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	var b strings.Builder
	for i := 0; i < 3; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(letras))))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteByte(letras[n.Int64()])
	}
	for i := 0; i < 3; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteByte(byte('0' + n.Int64()))
	}
	return b.String()
}

// payloadEnc arma el payload_enc de la app: {"patente", "chasis"?} cifrado con HPKE.
func (a *apiVinculacion) payloadEnc(patente string, conChasis bool) string {
	a.t.Helper()

	campos := map[string]any{"patente": []byte(patente)}
	if conChasis {
		campos["chasis"] = []byte("1HGCM82633A004352")
	}
	raw, err := json.Marshal(campos)
	if err != nil {
		a.t.Fatal(err)
	}
	sealed, err := a.encrypt.Encrypt(raw)
	if err != nil {
		a.t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}

type respuesta struct {
	status int
	cuerpo map[string]any
	header http.Header
}

func (a *apiVinculacion) llamar(metodo, ruta string, owner uuid.UUID, cuerpo any) respuesta {
	a.t.Helper()

	var reader *bytes.Reader
	if cuerpo == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(cuerpo)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(metodo, ruta, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", owner.String())

	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	a.bodies = append(a.bodies, rec.Body.String())
	resp := respuesta{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp.cuerpo); err != nil {
			a.t.Fatalf("%s %s: el cuerpo no es JSON: %q", metodo, ruta, rec.Body.String())
		}
	}
	return resp
}

func (a *apiVinculacion) vincular(owner uuid.UUID, patente string) respuesta {
	return a.llamar(http.MethodPost, "/api/v1/vehicles", owner, map[string]string{"payload_enc": a.payloadEnc(patente, false)})
}

func (a *apiVinculacion) desvincular(owner uuid.UUID, patente string) respuesta {
	return a.llamar(http.MethodPost, "/api/v1/vehicles/unlink", owner, map[string]string{"payload_enc": a.payloadEnc(patente, false)})
}

func (a *apiVinculacion) contar(owner uuid.UUID) respuesta {
	return a.llamar(http.MethodGet, "/api/v1/vehicles", owner, nil)
}

func (a *apiVinculacion) desvincularTodos(owner uuid.UUID) respuesta {
	return a.llamar(http.MethodDelete, "/api/v1/vehicles", owner, nil)
}

// duenos resuelve los dueños de la patente por el mismo camino que /internal/resolve-owner.
func (a *apiVinculacion) duenos(patente string) []uuid.UUID {
	a.t.Helper()

	// Las patentes de esta prueba se resuelven por su blind index, igual que Alert;
	// el secreto es el del servicio armado en nuevaAPI.
	hashed := blindIndexE2E(patente)
	owners, ref, err := a.repo.FindOwnersOrReservePending(context.Background(), hashed, domain.PendingProposal{
		Ref: uuid.New(), Now: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		a.t.Fatalf("no se pudo resolver los dueños: %v", err)
	}
	if ref != nil {
		a.db.Exec("DELETE FROM pending_plates WHERE pending_ref = ?", *ref)
	}
	return owners
}

func esperarEstado(t *testing.T, paso string, r respuesta, status int, code string) {
	t.Helper()

	if r.status != status {
		t.Fatalf("%s: status = %d, esperaba %d (%v)", paso, r.status, status, r.cuerpo)
	}
	if code != "" && r.cuerpo["code"] != code {
		t.Fatalf("%s: code = %v, esperaba %s", paso, r.cuerpo["code"], code)
	}
}

func esperarConteo(t *testing.T, paso string, r respuesta, status, count int) {
	t.Helper()

	esperarEstado(t, paso, r, status, "")
	if got, _ := r.cuerpo["count"].(float64); int(got) != count {
		t.Fatalf("%s: count = %v, esperaba %d", paso, r.cuerpo["count"], count)
	}
	if got, _ := r.cuerpo["max"].(float64); int(got) != topeE2E {
		t.Fatalf("%s: max = %v, esperaba %d", paso, r.cuerpo["max"], topeE2E)
	}
	if len(r.cuerpo) != 2 {
		t.Fatalf("%s: la respuesta debe tener solo count y max: %v", paso, r.cuerpo)
	}
}

// Recorre los escenarios E2E del contrato US-14 contra la API real.
func TestVinculacionPorDominioDePuntaAPunta(t *testing.T) {
	var logs bytes.Buffer
	anterior := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(anterior) })

	a := nuevaAPI(t)
	usuario, otro := a.usuario(), a.usuario()
	p1, p2, p3, p4, noVinculada := patenteAlAzar(t), patenteAlAzar(t), patenteAlAzar(t), patenteAlAzar(t), patenteAlAzar(t)

	// 1. Vincular: el contador sube y la respuesta no trae la patente.
	esperarConteo(t, "vincular 1", a.vincular(usuario, p1), http.StatusCreated, 1)
	esperarConteo(t, "contar", a.contar(usuario), http.StatusOK, 1)
	if owners := a.duenos(p1); len(owners) != 1 || owners[0] != usuario {
		t.Fatalf("la patente vinculada debía resolver al usuario, obtuvo %v", owners)
	}

	// 2. La misma patente otra vez, escrita distinto: duplicada.
	esperarEstado(t, "duplicada", a.vincular(usuario, strings.ToLower(p1[:3])+"-"+p1[3:]), http.StatusConflict, "DUPLICATE_VEHICLE")

	// Alta con chasis (app anterior): el chasis se ignora.
	conChasis := a.llamar(http.MethodPost, "/api/v1/vehicles", usuario, map[string]string{"payload_enc": a.payloadEnc(p2, true)})
	esperarConteo(t, "vincular con chasis", conChasis, http.StatusCreated, 2)
	esperarConteo(t, "vincular 3", a.vincular(usuario, p3), http.StatusCreated, 3)

	// 3. Un cuarto vehículo: tope.
	esperarEstado(t, "tope", a.vincular(usuario, p4), http.StatusConflict, "VEHICLE_LIMIT_REACHED")
	esperarConteo(t, "contar tras el tope", a.contar(usuario), http.StatusOK, 3)
	// Repetir una patente en el tope sigue siendo duplicado, no tope.
	esperarEstado(t, "duplicada en el tope", a.vincular(usuario, p1), http.StatusConflict, "DUPLICATE_VEHICLE")
	// El conteo es solo propio.
	esperarConteo(t, "otro usuario sin vehículos", a.contar(otro), http.StatusOK, 0)

	// 5. Desvincular una patente que no tiene.
	esperarEstado(t, "no vinculada", a.desvincular(usuario, noVinculada), http.StatusNotFound, "VEHICLE_NOT_FOUND")

	// 4. Desvincular escribiéndola: el contador baja, el lugar se libera y la patente
	// ya no resuelve al usuario.
	esperarConteo(t, "desvincular", a.desvincular(usuario, p1), http.StatusOK, 2)
	if owners := a.duenos(p1); len(owners) != 0 {
		t.Fatalf("la patente desvinculada no debía resolver a nadie, obtuvo %v", owners)
	}
	esperarConteo(t, "vincular tras liberar", a.vincular(usuario, p4), http.StatusCreated, 3)

	// 8. Dos usuarios con la misma patente: cada uno la tiene; si uno la desvincula,
	// el otro la conserva. Y desvincular una patente que solo tiene otro da 404.
	esperarConteo(t, "otro vincula p2", a.vincular(otro, p2), http.StatusCreated, 1)
	if owners := a.duenos(p2); len(owners) != 2 {
		t.Fatalf("la patente compartida debía resolver a los dos usuarios, obtuvo %v", owners)
	}
	esperarConteo(t, "otro desvincula p2", a.desvincular(otro, p2), http.StatusOK, 0)
	if owners := a.duenos(p2); len(owners) != 1 || owners[0] != usuario {
		t.Fatalf("el primer usuario debía conservar la patente, obtuvo %v", owners)
	}
	esperarEstado(t, "patente de otro", a.desvincular(otro, p3), http.StatusNotFound, "VEHICLE_NOT_FOUND")
	esperarConteo(t, "vínculo ajeno intacto", a.contar(usuario), http.StatusOK, 3)

	// 6. Desvincular todos: borra solo los propios y se puede volver a vincular.
	esperarConteo(t, "otro vincula p3", a.vincular(otro, p3), http.StatusCreated, 1)
	r := a.desvincularTodos(usuario)
	esperarEstado(t, "desvincular todos", r, http.StatusNoContent, "")
	esperarConteo(t, "contar tras desvincular todos", a.contar(usuario), http.StatusOK, 0)
	esperarConteo(t, "ajeno intacto", a.contar(otro), http.StatusOK, 1)
	esperarConteo(t, "volver a vincular", a.vincular(usuario, p1), http.StatusCreated, 1)

	// Formatos inválidos y criptograma inválido.
	for _, invalida := range []string{"AB12CD3", "1234567", "", "ABCD123"} {
		esperarEstado(t, "formato inválido "+invalida, a.vincular(usuario, invalida), http.StatusBadRequest, "INVALID_PLATE")
		esperarEstado(t, "desvincular formato inválido "+invalida, a.desvincular(usuario, invalida), http.StatusBadRequest, "INVALID_PLATE")
	}
	basura := a.llamar(http.MethodPost, "/api/v1/vehicles", usuario, map[string]string{"payload_enc": "esto-no-es-un-criptograma"})
	esperarEstado(t, "criptograma inválido", basura, http.StatusBadRequest, "INVALID_PLATE")

	// Ninguna respuesta ni log contiene ninguna de las patentes ni el chasis.
	secretos := []string{p1, p2, p3, p4, noVinculada, "1HGCM82633A004352"}
	for _, cuerpo := range a.bodies {
		for _, secreto := range secretos {
			if strings.Contains(cuerpo, secreto) {
				t.Fatalf("una respuesta contiene un dato sensible (%s): %s", secreto, cuerpo)
			}
		}
	}
	if logs.Len() == 0 {
		t.Fatal("la prueba no capturó ningún log: no verifica nada")
	}
	for _, secreto := range secretos {
		if strings.Contains(logs.String(), secreto) {
			t.Fatalf("el log contiene un dato sensible (%s)", secreto)
		}
	}
}

// 7. Una patente con alertas pendientes (sin dueño) las entrega al vincularla, y
// el rechazo por tope no las consume.
func TestVincularReclamaLasAlertasPendientesDeLaPatente(t *testing.T) {
	a := nuevaAPI(t)
	usuario := a.usuario()
	patente := patenteAlAzar(t)

	_, ref, err := a.repo.FindOwnersOrReservePending(context.Background(), blindIndexE2E(patente), domain.PendingProposal{
		Ref: uuid.New(), Now: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil || ref == nil {
		t.Fatalf("no se pudo reservar la patente pendiente: ref=%v err=%v", ref, err)
	}
	t.Cleanup(func() { a.db.Exec("DELETE FROM pending_plates WHERE pending_ref = ?", *ref) })

	esperarConteo(t, "vincular", a.vincular(usuario, patente), http.StatusCreated, 1)

	var eventos []struct {
		Actor   uuid.UUID
		Payload string
	}
	err = a.db.Raw("SELECT actor, payload::text AS payload FROM outbox_events WHERE event_type = ? AND payload->>'pending_ref' = ?",
		domain.EventTypePendingClaimed, ref.String()).Scan(&eventos).Error
	if err != nil {
		t.Fatalf("no se pudo leer el outbox: %v", err)
	}
	if len(eventos) != 1 || eventos[0].Actor != usuario {
		t.Fatalf("esperaba un vehicle.pending.claimed del usuario, obtuvo %+v", eventos)
	}
	t.Cleanup(func() { a.db.Exec("DELETE FROM outbox_events WHERE payload->>'pending_ref' = ?", ref.String()) })
}

func TestLasRutasDeVehiculosSinXUserIdResponden401ConCode(t *testing.T) {
	a := nuevaAPI(t)

	for _, ruta := range []struct{ metodo, path string }{
		{http.MethodPost, "/api/v1/vehicles"}, {http.MethodGet, "/api/v1/vehicles"},
		{http.MethodPost, "/api/v1/vehicles/unlink"}, {http.MethodDelete, "/api/v1/vehicles"},
	} {
		req := httptest.NewRequest(ruta.metodo, ruta.path, strings.NewReader(`{"payload_enc":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)

		var cuerpo map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &cuerpo)
		if rec.Code != http.StatusUnauthorized || cuerpo["code"] != "UNAUTHORIZED" {
			t.Fatalf("%s %s: %d %v", ruta.metodo, ruta.path, rec.Code, cuerpo)
		}
	}
}

// blindIndexE2E calcula el blind index de una patente canónica con el secreto con
// el que nuevaAPI arma el servicio.
func blindIndexE2E(patente string) string {
	return blindindex.Compute([]byte("secreto-e2e"), []byte(patente))
}
