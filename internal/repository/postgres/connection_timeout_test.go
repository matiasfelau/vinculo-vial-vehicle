package postgres

import (
	"net/url"
	"testing"
)

func TestWithConnectTimeoutAgregaElParametroSiFalta(t *testing.T) {
	dsn := "postgres://vehicle:vehicle@vinculo-vial-vehicle-db:5432/vehicle?sslmode=disable"

	resultado, err := withConnectTimeout(dsn, 5)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	parsed, err := url.Parse(resultado)
	if err != nil {
		t.Fatalf("el resultado no es una URL válida: %v", err)
	}
	if got := parsed.Query().Get("connect_timeout"); got != "5" {
		t.Fatalf("esperaba connect_timeout=5, obtuvo %q", got)
	}
	if got := parsed.Query().Get("sslmode"); got != "disable" {
		t.Fatalf("esperaba preservar sslmode=disable, obtuvo %q", got)
	}
}

func TestWithConnectTimeoutRespetaUnValorExplicito(t *testing.T) {
	dsn := "postgres://vehicle:vehicle@vinculo-vial-vehicle-db:5432/vehicle?sslmode=disable&connect_timeout=15"

	resultado, err := withConnectTimeout(dsn, 5)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	parsed, err := url.Parse(resultado)
	if err != nil {
		t.Fatalf("el resultado no es una URL válida: %v", err)
	}
	if got := parsed.Query().Get("connect_timeout"); got != "15" {
		t.Fatalf("esperaba preservar connect_timeout=15 explícito, obtuvo %q", got)
	}
}

func TestWithConnectTimeoutCadenaInvalidaRetornaError(t *testing.T) {
	// Un carácter de control en el DSN produce un error de parseo de url.Parse.
	if _, err := withConnectTimeout("postgres://\x7f", 5); err == nil {
		t.Fatal("esperaba error con una cadena de conexión no interpretable")
	}
}
