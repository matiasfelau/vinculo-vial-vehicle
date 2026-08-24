package dnrpa

import (
	"context"
	"testing"
)

func TestGetOwnerDNIRetornaElDNIConfigurado(t *testing.T) {
	adapter := NewMockDNRPAAdapter([]byte("12345678"))

	dni, err := adapter.GetOwnerDNI(context.Background(), []byte("AA000AA"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if string(dni) != "12345678" {
		t.Fatalf("esperaba DNI %q, obtuvo %q", "12345678", dni)
	}
}

func TestGetOwnerDNIEsIndependienteDeLaPatente(t *testing.T) {
	adapter := NewMockDNRPAAdapter([]byte("12345678"))

	a, err := adapter.GetOwnerDNI(context.Background(), []byte("AA000AA"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	b, err := adapter.GetOwnerDNI(context.Background(), []byte("BB111BB"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if string(a) != string(b) {
		t.Fatal("el mock debe retornar siempre el mismo DNI, sin importar la patente")
	}
}

func TestGetOwnerDNIRetornaCopiasIndependientesEntreLlamadas(t *testing.T) {
	adapter := NewMockDNRPAAdapter([]byte("12345678"))

	primero, err := adapter.GetOwnerDNI(context.Background(), []byte("AA000AA"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i := range primero {
		primero[i] = 0
	}

	segundo, err := adapter.GetOwnerDNI(context.Background(), []byte("AA000AA"))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if string(segundo) != "12345678" {
		t.Fatalf("purgar el resultado de una consulta no debe afectar consultas posteriores; obtuvo %q", segundo)
	}
}
