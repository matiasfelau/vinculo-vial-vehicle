package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/crypto/hpke"
	httpdelivery "github.com/matiasfelau/vinculo-vial-vehicle/internal/delivery/http"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/lookup"
)

// Prueba de US-16 con HPKE real, caso de uso real y router interno real, sin base de
// datos: dedup-key no la consulta. Actúa de Alert: cifra la patente con la clave pública
// de Vehicle y valida la respuesta contra la fórmula del contrato, calculada acá aparte.

const tokenDedupE2E = "token-interno-e2e"

type apiDedup struct {
	router  http.Handler
	encrypt *hpke.Encryptor
	key     []byte
}

func nuevaAPIDedup(t *testing.T) *apiDedup {
	t.Helper()

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

	key := bytes.Repeat([]byte{9}, 32)
	service := lookup.NewResolveOwnersService(nil, decryptor, nil, nil, "2026-11", []byte("pat-hmac-e2e"), key, time.Hour)

	gin.SetMode(gin.TestMode)
	return &apiDedup{router: httpdelivery.NewInternalRouter(tokenDedupE2E, service), encrypt: encryptor, key: key}
}

func (a *apiDedup) patEnc(t *testing.T, patente string) string {
	t.Helper()

	sealed, err := a.encrypt.Encrypt([]byte(patente))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}

func (a *apiDedup) post(t *testing.T, token string, body map[string]string) (int, map[string]string) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/dedup-key", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(httpdelivery.HeaderInternalServiceToken, token)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	var res map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, res
}

// esperada es la fórmula del contrato: HMAC-SHA256(key, issuer_id + ":" + patente), en hex.
func (a *apiDedup) esperada(issuer, patente string) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(issuer + ":" + patente))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestDedupKeyDePuntaAPuntaConHPKEReal(t *testing.T) {
	api := nuevaAPIDedup(t)
	const emisorA, emisorB = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"

	status, resA := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": api.patEnc(t, "AB123CD"), "issuer_id": emisorA})
	if status != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", status)
	}
	if want := api.esperada(emisorA, "AB123CD"); resA["dedup_key"] != want {
		t.Fatalf("dedup_key = %q, el contrato da %q", resA["dedup_key"], want)
	}

	// Cada cifrado HPKE es distinto, pero la clave es determinística por emisor y patente.
	_, again := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": api.patEnc(t, "AB123CD"), "issuer_id": emisorA})
	if again["dedup_key"] != resA["dedup_key"] {
		t.Fatal("la misma patente del mismo emisor dio claves distintas")
	}

	// La patente se normaliza: minúsculas y espacios son la misma patente.
	_, normalized := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": api.patEnc(t, "ab 123 cd"), "issuer_id": emisorA})
	if normalized["dedup_key"] != resA["dedup_key"] {
		t.Fatal("la patente no se normalizó antes de calcular la clave")
	}

	// Otro emisor a la misma patente: clave sin relación.
	_, resB := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": api.patEnc(t, "AB123CD"), "issuer_id": emisorB})
	if resB["dedup_key"] == resA["dedup_key"] || resB["dedup_key"] != api.esperada(emisorB, "AB123CD") {
		t.Fatal("la clave de otro emisor a la misma patente no cumple el contrato")
	}
}

func TestDedupKeyDePuntaAPuntaRechazos(t *testing.T) {
	api := nuevaAPIDedup(t)
	issuer := "11111111-1111-4111-8111-111111111111"

	t.Run("patente con formato inválido", func(t *testing.T) {
		status, res := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": api.patEnc(t, "no-es-patente"), "issuer_id": issuer})
		if status != http.StatusBadRequest || res["code"] != "INVALID_PLATE" || res["dedup_key"] != "" {
			t.Fatalf("esperaba 400 INVALID_PLATE, obtuvo %d %v", status, res)
		}
	})
	t.Run("criptograma que no abre", func(t *testing.T) {
		status, res := api.post(t, tokenDedupE2E, map[string]string{"pat_enc": base64.StdEncoding.EncodeToString([]byte("basura")), "issuer_id": issuer})
		if status != http.StatusBadRequest || res["code"] != "INVALID_PLATE" {
			t.Fatalf("esperaba 400 INVALID_PLATE, obtuvo %d %v", status, res)
		}
	})
	t.Run("sin token de servicio", func(t *testing.T) {
		status, _ := api.post(t, "", map[string]string{"pat_enc": api.patEnc(t, "AB123CD"), "issuer_id": issuer})
		if status != http.StatusUnauthorized {
			t.Fatalf("esperaba 401, obtuvo %d", status)
		}
	})
}
