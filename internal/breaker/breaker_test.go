package breaker

import (
	"testing"
	"time"
)

// nuevoConReloj construye un Breaker con un reloj controlado por la prueba,
// para no depender de tiempos reales de espera.
func nuevoConReloj(threshold int, window, openDuration time.Duration, ahora *time.Time) *Breaker {
	b := New(threshold, window, openDuration)
	b.now = func() time.Time { return *ahora }
	return b
}

func TestAllowPermiteEnEstadoCerrado(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(3, time.Minute, time.Second, &ahora)

	if !b.Allow() {
		t.Fatal("esperaba permitir la llamada en estado cerrado")
	}
}

func TestSeAbreAlAlcanzarElUmbralDeFallas(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(3, time.Minute, time.Second, &ahora)

	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("falla #%d: esperaba permitir la llamada antes de alcanzar el umbral", i+1)
		}
		b.RecordFailure()
	}

	if b.Allow() {
		t.Fatal("esperaba el circuito abierto tras alcanzar el umbral de fallas")
	}
}

func TestNoSeAbreSiNoSeAlcanzaElUmbral(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(3, time.Minute, time.Second, &ahora)

	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	if !b.Allow() {
		t.Fatal("esperaba el circuito cerrado con fallas por debajo del umbral")
	}
}

func TestUnExitoLimpiaElConteoDeFallas(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(3, time.Minute, time.Second, &ahora)

	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	b.Allow()
	b.RecordSuccess()

	// Dos fallas nuevas no deberían alcanzar el umbral: el éxito reinició el conteo.
	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	if !b.Allow() {
		t.Fatal("esperaba el circuito cerrado: el éxito debió limpiar las fallas previas")
	}
}

func TestUnaFallaFueraDeLaVentanaReiniciaElConteo(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(3, time.Minute, time.Second, &ahora)

	b.Allow()
	b.RecordFailure()

	ahora = ahora.Add(2 * time.Minute) // excede la ventana de un minuto

	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	// Solo 2 fallas contabilizadas dentro de la ventana vigente: no alcanza el umbral de 3.
	if !b.Allow() {
		t.Fatal("esperaba el circuito cerrado: la falla anterior quedó fuera de la ventana")
	}
}

func TestConCircuitoAbiertoFallaDeInmediatoSinAdmitirLaLlamada(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(1, time.Minute, 10*time.Second, &ahora)

	b.Allow()
	b.RecordFailure() // abre el circuito

	if b.Allow() {
		t.Fatal("esperaba el circuito abierto rechazando la llamada")
	}

	ahora = ahora.Add(5 * time.Second) // aún dentro de openDuration

	if b.Allow() {
		t.Fatal("esperaba el circuito abierto rechazando la llamada antes de openDuration")
	}
}

func TestTranscurridoOpenDurationAdmiteUnaLlamadaDePrueba(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(1, time.Minute, 10*time.Second, &ahora)

	b.Allow()
	b.RecordFailure() // abre el circuito

	ahora = ahora.Add(11 * time.Second) // supera openDuration

	if !b.Allow() {
		t.Fatal("esperaba admitir la llamada de prueba tras transcurrir openDuration")
	}
}

func TestLlamadaDePruebaConcurrenteFallaDeInmediato(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(1, time.Minute, 10*time.Second, &ahora)

	b.Allow()
	b.RecordFailure() // abre el circuito
	ahora = ahora.Add(11 * time.Second)

	if !b.Allow() {
		t.Fatal("esperaba admitir la primera llamada de prueba")
	}
	// Antes de registrar el resultado de la prueba en curso, otra llamada
	// concurrente no debe admitirse.
	if b.Allow() {
		t.Fatal("esperaba rechazar una segunda llamada mientras la prueba está en curso")
	}
}

func TestLaLlamadaDePruebaExitosaCierraElCircuito(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(1, time.Minute, 10*time.Second, &ahora)

	b.Allow()
	b.RecordFailure()
	ahora = ahora.Add(11 * time.Second)

	b.Allow()
	b.RecordSuccess()

	if !b.Allow() {
		t.Fatal("esperaba el circuito cerrado tras una prueba exitosa")
	}
	b.RecordFailure()
	if b.Allow() {
		t.Fatal("esperaba que una nueva falla vuelva a abrir el circuito ya cerrado (umbral 1)")
	}
}

func TestLaLlamadaDePruebaFallidaReabreElCircuitoDeInmediato(t *testing.T) {
	ahora := time.Now()
	b := nuevoConReloj(1, time.Minute, 10*time.Second, &ahora)

	b.Allow()
	b.RecordFailure()
	ahora = ahora.Add(11 * time.Second)

	b.Allow()
	b.RecordFailure() // la prueba también falla

	if b.Allow() {
		t.Fatal("esperaba el circuito abierto de nuevo tras una prueba fallida")
	}

	ahora = ahora.Add(11 * time.Second) // debe volver a esperar openDuration completo

	if !b.Allow() {
		t.Fatal("esperaba admitir una nueva prueba tras openDuration desde la reapertura")
	}
}
