package blindindex

import "testing"

func TestComputeEsDeterministico(t *testing.T) {
	secret := []byte("secreto-de-prueba")
	plate := []byte("AA000AA")

	a := Compute(secret, plate)
	b := Compute(secret, plate)

	if a != b {
		t.Fatalf("Compute no es determinístico: %q != %q", a, b)
	}
}

func TestComputeDifiereConPatenteDistinta(t *testing.T) {
	secret := []byte("secreto-de-prueba")

	a := Compute(secret, []byte("AA000AA"))
	b := Compute(secret, []byte("BB111BB"))

	if a == b {
		t.Fatal("Compute produjo el mismo índice para patentes distintas")
	}
}

func TestComputeDifiereConSecretoDistinto(t *testing.T) {
	plate := []byte("AA000AA")

	a := Compute([]byte("secreto-uno"), plate)
	b := Compute([]byte("secreto-dos"), plate)

	if a == b {
		t.Fatal("Compute produjo el mismo índice con secretos distintos")
	}
}
