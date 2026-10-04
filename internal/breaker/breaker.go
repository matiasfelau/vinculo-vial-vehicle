// Package breaker implementa un circuit breaker de fail-fast genérico, sin
// conocimiento de ninguna dependencia concreta: quien lo instancia decide qué
// constituye una falla contabilizable para su propio destino (ver
// internal/client/identity).
package breaker

import (
	"sync"
	"time"
)

// state representa la máquina de tres estados del breaker.
type state int

const (
	closed state = iota
	open
	halfOpen
)

// Breaker es un circuit breaker con umbral de fallas, ventana de conteo y
// tiempo de apertura configurables. Es seguro para uso concurrente.
//
// En estado cerrado, las fallas contabilizables se acumulan mientras ocurran
// dentro de window; alcanzado threshold, el circuito se abre. En estado
// abierto, toda llamada falla de inmediato sin alcanzar al destino hasta que
// transcurre openDuration, momento en que se admite una única llamada de
// prueba (half-open): si tiene éxito el circuito se cierra, si falla se
// reabre de inmediato.
type Breaker struct {
	mu           sync.Mutex
	state        state
	threshold    int
	window       time.Duration
	openDuration time.Duration
	now          func() time.Time

	failures    int
	windowStart time.Time
	openedAt    time.Time
}

// New construye un Breaker cerrado. threshold es la cantidad de fallas
// contabilizables dentro de window que abren el circuito; openDuration es el
// tiempo que el circuito permanece abierto antes de admitir la llamada de
// prueba.
func New(threshold int, window, openDuration time.Duration) *Breaker {
	return &Breaker{
		threshold:    threshold,
		window:       window,
		openDuration: openDuration,
		now:          time.Now,
		state:        closed,
	}
}

// Allow indica si una llamada puede proceder hacia el destino. Retorna false
// con el circuito abierto: el llamador debe fallar de inmediato, sin invocar
// al destino, hasta que se cumpla openDuration.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case open:
		if b.now().Sub(b.openedAt) < b.openDuration {
			return false
		}
		// Transcurrió openDuration: se admite una única llamada de prueba.
		b.state = halfOpen
		return true
	case halfOpen:
		// Ya hay una llamada de prueba en curso; las concurrentes fallan rápido.
		return false
	default: // closed
		return true
	}
}

// RecordSuccess reporta que la llamada admitida por Allow tuvo éxito. Cierra
// el circuito y limpia el conteo de fallas, tanto si la llamada ocurrió en
// estado cerrado como si fue la prueba de un estado half-open.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = closed
	b.failures = 0
	b.windowStart = time.Time{}
}

// RecordFailure reporta que la llamada admitida por Allow falló de una forma
// contabilizable para esta dependencia. Si la falla ocurrió durante la
// llamada de prueba en half-open, el circuito se reabre de inmediato. En
// estado cerrado, la falla se acumula dentro de window; si una falla previa
// quedó fuera de la ventana, el conteo se reinicia antes de sumarla.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == halfOpen {
		b.state = open
		b.openedAt = b.now()
		b.failures = 0
		return
	}

	now := b.now()
	if b.failures == 0 || now.Sub(b.windowStart) > b.window {
		b.windowStart = now
		b.failures = 1
	} else {
		b.failures++
	}

	if b.failures >= b.threshold {
		b.state = open
		b.openedAt = now
		b.failures = 0
	}
}

// State devuelve el estado efectivo del circuito como texto: "closed", "open"
// o "half-open". Un circuito abierto cuyo tiempo de apertura ya transcurrió se
// reporta como "half-open", porque la próxima llamada será la prueba. A
// diferencia de Allow, consultar el estado no modifica el breaker.
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case open:
		if b.now().Sub(b.openedAt) >= b.openDuration {
			return "half-open"
		}
		return "open"
	case halfOpen:
		return "half-open"
	default:
		return "closed"
	}
}
