package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

type fakeResolveOwnersUseCase struct {
	ownerIDs        []uuid.UUID
	patEncIA        string
	patEncAudit     string
	auditKeyID      string
	pendingRef      *uuid.UUID
	dedupKey        string
	err             error
	receivedPatEnc  string
	receivedAlertID uuid.UUID
	receivedIssuer  string
	fueInvocado     bool

	dedupKeyErr      error
	dedupKeyInvocado bool
	dedupKeyPatEnc   string
	dedupKeyIssuer   string
}

func (f *fakeResolveOwnersUseCase) DedupKey(patEnc, issuerID string) (string, error) {
	f.dedupKeyInvocado = true
	f.dedupKeyPatEnc = patEnc
	f.dedupKeyIssuer = issuerID
	return f.dedupKey, f.dedupKeyErr
}

func (f *fakeResolveOwnersUseCase) ResolveOwners(ctx context.Context, patEnc string, alertID uuid.UUID, issuerID string) (domain.OwnerResolution, error) {
	f.fueInvocado = true
	f.receivedIssuer = issuerID
	f.receivedPatEnc = patEnc
	f.receivedAlertID = alertID
	return domain.OwnerResolution{
		OwnerIDs:    f.ownerIDs,
		PatEncIA:    f.patEncIA,
		PatEncAudit: f.patEncAudit,
		AuditKeyID:  f.auditKeyID,
		PendingRef:  f.pendingRef,
		DedupKey:    f.dedupKey,
	}, f.err
}

// validBody arma un request válido; los casos de error lo parten a mano.
func validBody() map[string]string {
	return map[string]string{"pat_enc": "criptograma", "alert_id": uuid.NewString()}
}

func resolveOwnerRequestBody(t *testing.T, body any) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("no se pudo serializar el cuerpo de la solicitud: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/resolve-owner", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderInternalServiceToken, secretoDePrueba)
	return req
}

func TestResolveOwnerPatenteConTresDuenosRetorna200ConLosTresUUIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owners := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	resolver := &fakeResolveOwnersUseCase{ownerIDs: owners, patEncIA: "cifrado-para-ia"}
	router := NewInternalRouter(secretoDePrueba, resolver)
	body := validBody()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}

	var res resolveOwnerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if len(res.OwnerIDs) != 3 {
		t.Fatalf("esperaba 3 owner_ids, obtuvo %d", len(res.OwnerIDs))
	}
	if res.PatEncIA != "cifrado-para-ia" {
		t.Fatalf("esperaba pat_enc_ia en la respuesta, obtuvo %q", res.PatEncIA)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("pending_ref")) {
		t.Fatalf("con dueños pending_ref debía omitirse, obtuvo %s", rec.Body.String())
	}
	if resolver.receivedPatEnc != "criptograma" {
		t.Fatal("el pat_enc recibido no coincide con el enviado al caso de uso")
	}
	if resolver.receivedAlertID.String() != body["alert_id"] {
		t.Fatal("el alert_id recibido no coincide con el enviado al caso de uso")
	}
}

func TestResolveOwnerPatenteSinDuenosRetorna200ConArrayVacio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{ownerIDs: nil, patEncIA: "cifrado-para-ia"}
	router := NewInternalRouter(secretoDePrueba, resolver)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}
	// El contrato exige un array vacío, nunca null: Alert no debe tratar la
	// ausencia de dueños como un error de deserialización.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"owner_ids":[]`)) {
		t.Fatalf("esperaba owner_ids como array vacío, obtuvo %s", rec.Body.String())
	}
	// IA valida la patente también cuando nadie la vinculó (US-10).
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"pat_enc_ia":"cifrado-para-ia"`)) {
		t.Fatalf("pat_enc_ia debía ir también sin dueños, obtuvo %s", rec.Body.String())
	}
}

func TestResolveOwnerPatenteSinDuenosIncluyePendingRef(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pendingRef := uuid.New()
	resolver := &fakeResolveOwnersUseCase{patEncIA: "cifrado-para-ia", pendingRef: &pendingRef}
	router := NewInternalRouter(secretoDePrueba, resolver)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}
	var res resolveOwnerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if len(res.OwnerIDs) != 0 {
		t.Fatalf("esperaba owner_ids vacío, obtuvo %v", res.OwnerIDs)
	}
	if res.PendingRef == nil || *res.PendingRef != pendingRef {
		t.Fatalf("esperaba pending_ref %v, obtuvo %v", pendingRef, res.PendingRef)
	}
}

// pat_enc_audit y audit_key_id (US-11) van siempre, con y sin dueños, y no
// desplazan a los campos existentes.
func TestResolveOwnerIncluyePatEncAuditYAuditKeyIDConYSinDuenos(t *testing.T) {
	casos := map[string][]uuid.UUID{
		"con dueños": {uuid.New()},
		"sin dueños": nil,
	}

	for nombre, owners := range casos {
		t.Run(nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			resolver := &fakeResolveOwnersUseCase{
				ownerIDs:    owners,
				patEncIA:    "cifrado-para-ia",
				patEncAudit: "cifrado-para-audit",
				auditKeyID:  "2026-11",
			}
			router := NewInternalRouter(secretoDePrueba, resolver)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

			if rec.Code != http.StatusOK {
				t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
			}
			var res map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("la respuesta no es JSON: %v", err)
			}
			if res["pat_enc_audit"] != "cifrado-para-audit" {
				t.Fatalf("esperaba pat_enc_audit, obtuvo %v", res["pat_enc_audit"])
			}
			if res["audit_key_id"] != "2026-11" {
				t.Fatalf("esperaba audit_key_id, obtuvo %v", res["audit_key_id"])
			}
			if res["pat_enc_ia"] != "cifrado-para-ia" {
				t.Fatalf("pat_enc_ia no debía cambiar, obtuvo %v", res["pat_enc_ia"])
			}
			if _, ok := res["owner_ids"].([]any); !ok {
				t.Fatalf("owner_ids debía seguir siendo un array, obtuvo %v", res["owner_ids"])
			}
		})
	}
}

// Ni la patente ni los criptogramas deben llegar a los logs, en éxito ni en error.
func TestResolveOwnerNoLogueaLaPatenteNiLosCriptogramas(t *testing.T) {
	const (
		patente     = "AB123CD"
		patEnc      = "cripto-de-entrada-secreto"
		patEncIA    = "cripto-ia-secreto"
		patEncAudit = "cripto-audit-secreto"
	)
	casos := map[string]error{
		"éxito":              nil,
		"error interno":      errors.New("fallo de cifrado"),
		"base no disponible": domain.ErrRepositoryUnavailable,
		"patente inválida":   domain.ErrInvalidPlate,
	}

	for nombre, errCasoDeUso := range casos {
		t.Run(nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var logs bytes.Buffer
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			resolver := &fakeResolveOwnersUseCase{patEncIA: patEncIA, patEncAudit: patEncAudit, auditKeyID: "2026-11", err: errCasoDeUso}
			router := NewInternalRouter(secretoDePrueba, resolver)
			body := map[string]string{"pat_enc": patEnc, "alert_id": uuid.NewString()}

			router.ServeHTTP(httptest.NewRecorder(), resolveOwnerRequestBody(t, body))

			for _, secreto := range []string{patente, patEnc, patEncIA, patEncAudit} {
				if strings.Contains(logs.String(), secreto) {
					t.Fatalf("el log no debía contener %q: %s", secreto, logs.String())
				}
			}
		})
	}
}

func TestResolveOwnerSinElSecretoDeServicioDevuelve401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{ownerIDs: []uuid.UUID{uuid.New()}}
	router := NewInternalRouter(secretoDePrueba, resolver)

	req := httptest.NewRequest(http.MethodPost, "/internal/resolve-owner",
		bytes.NewReader([]byte(`{"pat_enc":"criptograma"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
	}
	if resolver.fueInvocado {
		t.Fatal("no debía invocarse el caso de uso sin el secreto de servicio")
	}
}

func TestResolveOwnerPayloadMalFormadoRetorna400(t *testing.T) {
	casos := []struct {
		nombre string
		body   map[string]string
	}{
		{"pat_enc vacio", map[string]string{"pat_enc": "", "alert_id": uuid.NewString()}},
		{"pat_enc ausente", map[string]string{"alert_id": uuid.NewString()}},
		{"alert_id ausente", map[string]string{"pat_enc": "criptograma"}},
		{"alert_id vacio", map[string]string{"pat_enc": "criptograma", "alert_id": ""}},
		{"alert_id no es UUID", map[string]string{"pat_enc": "criptograma", "alert_id": "no-es-un-uuid"}},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			resolver := &fakeResolveOwnersUseCase{}
			router := NewInternalRouter(secretoDePrueba, resolver)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, resolveOwnerRequestBody(t, c.body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
			}
			if resolver.fueInvocado {
				t.Fatal("no debía invocarse el caso de uso con payload mal formado")
			}
		})
	}
}

func TestResolveOwnerErroresDeFormatoDelCasoDeUsoRetorna400(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
	}{
		{"patente invalida", domain.ErrInvalidPlate},
		{"criptograma invalido", domain.ErrInvalidCryptogram},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			resolver := &fakeResolveOwnersUseCase{err: c.err}
			router := NewInternalRouter(secretoDePrueba, resolver)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
			}
		})
	}
}

func TestResolveOwnerBaseDeDatosInalcanzableRetorna503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{err: domain.ErrRepositoryUnavailable}
	router := NewInternalRouter(secretoDePrueba, resolver)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperaba 503, obtuvo %d", rec.Code)
	}
}

func TestResolveOwnerErrorInternoRetorna500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{err: errors.New("fallo de base de datos")}
	router := NewInternalRouter(secretoDePrueba, resolver)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("esperaba 500, obtuvo %d", rec.Code)
	}
}

func TestResolveOwnerConIssuerIDDevuelveDedupKeyYSeLaPasaAlCasoDeUso(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{patEncIA: "cifrado-para-ia", dedupKey: "ab12"}
	router := NewInternalRouter(secretoDePrueba, resolver)
	body := validBody()
	body["issuer_id"] = "emisor-1"

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}
	var res resolveOwnerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if res.DedupKey != "ab12" {
		t.Fatalf("esperaba dedup_key ab12, obtuvo %q", res.DedupKey)
	}
	if resolver.receivedIssuer != "emisor-1" {
		t.Fatalf("el caso de uso recibió issuer_id %q", resolver.receivedIssuer)
	}
	if res.PatEncIA != "cifrado-para-ia" {
		t.Fatal("la respuesta debe conservar el resto de los campos")
	}
}

func TestResolveOwnerSinIssuerIDOmiteDedupKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := &fakeResolveOwnersUseCase{patEncIA: "cifrado-para-ia"}
	router := NewInternalRouter(secretoDePrueba, resolver)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, resolveOwnerRequestBody(t, validBody()))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d (%s)", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("dedup_key")) {
		t.Fatalf("sin issuer_id la respuesta queda como antes, obtuvo %s", rec.Body.String())
	}
	if resolver.receivedIssuer != "" {
		t.Fatalf("el caso de uso recibió issuer_id %q sin que llegara", resolver.receivedIssuer)
	}
}
