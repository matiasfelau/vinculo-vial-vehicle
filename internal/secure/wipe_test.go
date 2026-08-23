package secure

import "testing"

func TestWipeSobrescribeTodosLosBytes(t *testing.T) {
	data := []byte("patente-en-claro-AA000AA")

	Wipe(data)

	for i, b := range data {
		if b != 0 {
			t.Fatalf("byte en posición %d no fue sobrescrito: %d", i, b)
		}
	}
}

func TestWipeSliceVacio(t *testing.T) {
	data := []byte{}

	Wipe(data)
}

func TestWipeNil(t *testing.T) {
	var data []byte

	Wipe(data)
}
