package events

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ConnectionStatus expone, de forma segura para uso concurrente, si un
// consumidor está conectado al broker en este momento. RunWithReconnect lo
// actualiza en cada transición; /health/deps lo consulta. El valor cero
// reporta "no conectado".
type ConnectionStatus struct {
	connected atomic.Bool
}

// Connected indica si el consumidor tiene conexión y canal vivos.
func (s *ConnectionStatus) Connected() bool {
	return s.connected.Load()
}

// setConnected tolera un status nil para que quien no necesita observar el
// estado (por ejemplo, los tests) pueda pasar nil.
func setConnected(status *ConnectionStatus, connected bool) {
	if status == nil {
		return
	}
	status.connected.Store(connected)
}

// ConsumerFactory arma un Consumer sobre un canal recién abierto. Se invoca
// en cada conexión exitosa: nunca debe declarar exchanges, colas ni
// bindings, solo consumir y publicar sobre la topología de Core.
type ConsumerFactory func(channel ConsumerChannel) *Consumer

// closer es lo que RunWithReconnect necesita de una conexión para
// cerrarla al terminar.
type closer interface {
	Close() error
}

// dialFunc abre una conexión y un canal ya configurados.
type dialFunc func() (closer, ConsumerChannel, error)

// RunWithReconnect mantiene un consumidor activo ante caídas del broker. Ante
// un error de conexión o la pérdida del canal espera un backoff exponencial
// acotado y reintenta, sin volver a declarar topología. Devuelve cuando el
// contexto se cancela, una vez que el consumidor terminó de procesar el
// mensaje en vuelo.
//
// prefetch limita, vía basic.qos, cuántos mensajes sin ack puede tener en
// vuelo el canal. Sin ese límite el broker empuja toda la cola pendiente al
// proceso: con ack manual, memoria sin techo y, ante un crash, la devolución
// de todo el lote en vuelo de una vez.
//
// status, si no es nil, se actualiza en cada transición de conexión.
func RunWithReconnect(ctx context.Context, amqpURL, queue string, prefetch int, status *ConnectionStatus, factory ConsumerFactory) {
	runWithReconnect(ctx, func() (closer, ConsumerChannel, error) {
		return dialConsumer(amqpURL, prefetch)
	}, queue, status, factory, backoffLimits{min: reconnectInitialDelay, max: reconnectMaxDelay})
}

// backoffLimits acota la espera entre reintentos de conexión. Se inyecta
// para poder probar la reconexión sin esperas reales de segundos.
type backoffLimits struct {
	min, max time.Duration
}

func runWithReconnect(ctx context.Context, open dialFunc, queue string, status *ConnectionStatus, factory ConsumerFactory, limits backoffLimits) {
	backoff := limits.min
	defer setConnected(status, false)

	for ctx.Err() == nil {
		conn, channel, err := open()
		if err != nil {
			setConnected(status, false)
			slog.Error("consumidor: no se pudo conectar a rabbitmq", "queue", queue, "error", err.Error())
			if !sleepOrDone(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, limits.max)
			continue
		}

		setConnected(status, true)
		backoff = limits.min

		consumer := factory(channel)
		runErr := consumer.Run(ctx)

		setConnected(status, false)
		if err := channel.Close(); err != nil {
			slog.Debug("consumidor: error al cerrar el canal", "queue", queue, "error", err.Error())
		}
		if err := conn.Close(); err != nil {
			slog.Debug("consumidor: error al cerrar la conexión", "queue", queue, "error", err.Error())
		}

		if ctx.Err() != nil {
			return
		}

		if runErr != nil {
			slog.Error("consumidor: interrumpido, reconectando", "queue", queue, "error", runErr.Error())
		}

		if !sleepOrDone(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, limits.max)
	}
}

func dialConsumer(amqpURL string, prefetch int) (closer, ConsumerChannel, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, nil, err
	}

	rawChannel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	channel := realConsumerChannel{Channel: rawChannel}

	if err := configureChannel(channel, prefetch); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, nil, err
	}

	return conn, channel, nil
}

// configureChannel aplica sobre el canal la configuración que todo
// consumidor necesita: prefetch acotado (basic.qos), modo confirm (requerido
// para que AMQPDeadLetterPublisher espere la confirmación antes del ack del
// original) y el listener de devoluciones. Separado de dialConsumer para poder
// probarlo con un doble.
func configureChannel(channel ConsumerChannel, prefetch int) error {
	if err := channel.Qos(prefetch, 0, false); err != nil {
		return err
	}

	if err := channel.Confirm(false); err != nil {
		return err
	}

	go watchReturns(channel.NotifyReturn(make(chan amqp.Return, 1)))

	return nil
}

// sleepOrDone espera d, o hasta que el contexto se cancele. Devuelve false
// si la espera terminó por cancelación.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}
