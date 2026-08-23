package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

func TestVerifyOwnershipMatchTrue(t *testing.T) {
	ownerID := uuid.New()
	dniEnc := []byte("criptograma-dni")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got verifyOwnershipRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("no se pudo decodificar el cuerpo de la solicitud: %v", err)
		}
		if got.OwnerID != ownerID.String() {
			t.Errorf("owner_id esperado %q, obtuvo %q", ownerID.String(), got.OwnerID)
		}
		if got.DNIEnc != base64.StdEncoding.EncodeToString(dniEnc) {
			t.Errorf("dni_enc no coincide con el criptograma reenviado sin abrir")
		}
		if got := r.Header.Get("X-Internal-Service-Token"); got != "token-de-prueba" {
			t.Errorf("X-Internal-Service-Token esperado %q, obtuvo %q", "token-de-prueba", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(verifyOwnershipResponse{Match: true})
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-de-prueba", time.Second)

	match, err := client.VerifyOwnership(context.Background(), ownerID, dniEnc)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if !match {
		t.Fatal("esperaba match: true")
	}
}

func TestVerifyOwnershipMatchFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(verifyOwnershipResponse{Match: false})
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-de-prueba", time.Second)

	match, err := client.VerifyOwnership(context.Background(), uuid.New(), []byte("criptograma-dni"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if match {
		t.Fatal("esperaba match: false")
	}
}

func TestVerifyOwnershipTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(verifyOwnershipResponse{Match: true})
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-de-prueba", 5*time.Millisecond)

	_, err := client.VerifyOwnership(context.Background(), uuid.New(), []byte("criptograma-dni"))
	if !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnershipUnavailable por timeout, obtuvo %v", err)
	}
}

func TestVerifyOwnershipEstadoInesperado(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-de-prueba", time.Second)

	_, err := client.VerifyOwnership(context.Background(), uuid.New(), []byte("criptograma-dni"))
	if !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnershipUnavailable ante estado 500, obtuvo %v", err)
	}
}

func TestVerifyOwnershipRespuestaMalformada(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("no es json"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-de-prueba", time.Second)

	_, err := client.VerifyOwnership(context.Background(), uuid.New(), []byte("criptograma-dni"))
	if !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnershipUnavailable ante respuesta malformada, obtuvo %v", err)
	}
}

func TestVerifyOwnershipServidorInalcanzable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := NewClient(server.URL, "token-de-prueba", time.Second)

	_, err := client.VerifyOwnership(context.Background(), uuid.New(), []byte("criptograma-dni"))
	if !errors.Is(err, domain.ErrOwnershipUnavailable) {
		t.Fatalf("esperaba domain.ErrOwnershipUnavailable con servidor inalcanzable, obtuvo %v", err)
	}
}
