package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	reconnectInitialDelay = 1 * time.Second
	reconnectMaxDelay     = 30 * time.Second
)

// errBrokerUnreachable indica que no se pudo establecer la conexión TCP/AMQP con
// el broker (dirección inválida, broker caído, red inalcanzable). No es un error
// de despliegue: Connect no falla por esto, arranca en modo degradado
// (Healthy()=false) y reintenta en background con backoff.
var errBrokerUnreachable = errors.New("rabbitmq: broker inalcanzable")

// errExchangeNotFound indica que el exchange no existe o no coincide con lo
// esperado (tipo, durabilidad). Es un error de despliegue —Core no lo declaró, o
// lo declaró distinto—, así que Connect sí falla y main aborta el arranque.
var errExchangeNotFound = errors.New("rabbitmq: exchange no encontrado")

// Connection mantiene viva una conexión y un canal AMQP, reconectando en segundo
// plano ante una caída del broker en lugar de dejar el servicio publicando contra
// un canal muerto. Implementa amqpChannel delegando en el canal vigente, de modo
// que Publisher nunca retiene una referencia que una reconexión pueda dejar
// obsoleta.
type Connection struct {
	url      string
	exchange string

	mu      sync.RWMutex
	conn    *amqp.Connection
	channel *amqp.Channel
	healthy bool

	done      chan struct{}
	closeOnce sync.Once
}

// Connect abre la conexión y el canal iniciales y verifica que exchange ya
// exista. Dos desenlaces posibles ante una falla:
//   - El exchange no existe o no coincide con lo esperado: es un error de
//     despliegue. Connect falla y el llamador aborta el arranque.
//   - El broker es inalcanzable (TCP/AMQP): no es un error de despliegue. Connect
//     NO falla: devuelve una Connection en modo degradado (Healthy()=false) y
//     arranca el loop de reconexión en background, para que las rutas que no
//     publican sigan sirviendo. Los eventos quedan en el outbox hasta que el
//     broker vuelva.
func Connect(url, exchange string) (*Connection, error) {
	conn, channel, err := dial(url, exchange)
	if err != nil {
		if errors.Is(err, errExchangeNotFound) {
			return nil, err
		}

		slog.Error("rabbitmq: broker inalcanzable al arrancar, continuando en modo degradado", "error", err.Error())
		c := &Connection{url: url, exchange: exchange, done: make(chan struct{})}
		go c.reconnectLoop()
		return c, nil
	}

	c := &Connection{url: url, exchange: exchange, conn: conn, channel: channel, healthy: true, done: make(chan struct{})}
	go c.watch(conn, channel)
	go watchReturns(channel.NotifyReturn(make(chan amqp.Return, 1)))

	return c, nil
}

// dial abre la conexión y el canal, activa publisher confirms y verifica
// pasivamente que exchange exista. La topología la declara solo
// vinculo-vial-core: este servicio nunca crea infraestructura, así que una
// declaración activa (que crearía el exchange con una configuración adivinada)
// sería incorrecta. La declaración pasiva solo verifica que exista con el tipo y
// las propiedades esperadas, y falla si no.
func dial(url, exchange string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errBrokerUnreachable, err)
	}

	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w: error al abrir el canal: %w", errBrokerUnreachable, err)
	}

	if err := channel.Confirm(false); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w: error al activar publisher confirms: %w", errBrokerUnreachable, err)
	}

	if err := channel.ExchangeDeclarePassive(exchange, "topic", true, false, false, false, nil); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w %q (lo declara vinculo-vial-core): %w", errExchangeNotFound, exchange, err)
	}

	return conn, channel, nil
}

// watchReturns loguea en WARN cada mensaje que el broker devuelve por no poder
// enrutarlo (mandatory=true sin cola destino). Nunca incluye el cuerpo del
// mensaje. Termina cuando el canal se cierra, así que se relanza en cada
// (re)conexión junto con watch.
func watchReturns(returns <-chan amqp.Return) {
	for ret := range returns {
		slog.Warn("rabbitmq: mensaje no enrutable, el broker lo devolvió",
			"exchange", ret.Exchange,
			"routing_key", ret.RoutingKey,
			"reply_code", ret.ReplyCode,
			"reply_text", ret.ReplyText,
		)
	}
}

// watch espera el cierre de la conexión o del canal vigentes y dispara la
// reconexión. Se relanza sobre la conexión nueva tras cada reconexión exitosa.
func (c *Connection) watch(conn *amqp.Connection, channel *amqp.Channel) {
	connClosed := conn.NotifyClose(make(chan *amqp.Error, 1))
	chanClosed := channel.NotifyClose(make(chan *amqp.Error, 1))

	var closeErr *amqp.Error
	select {
	case closeErr = <-connClosed:
	case closeErr = <-chanClosed:
	case <-c.done:
		return
	}

	// Un cierre pedido por Close no es una caída del broker.
	select {
	case <-c.done:
		return
	default:
	}

	c.setHealthy(false)
	if closeErr != nil {
		slog.Error("rabbitmq: conexión perdida", "error", closeErr.Error())
	} else {
		slog.Error("rabbitmq: conexión cerrada")
	}

	c.reconnectLoop()
}

func (c *Connection) reconnectLoop() {
	delay := reconnectInitialDelay
	for {
		select {
		case <-c.done:
			return
		case <-time.After(delay):
		}

		conn, channel, err := dial(c.url, c.exchange)
		if err != nil {
			slog.Error("rabbitmq: fallo al reconectar, reintentando", "error", err.Error(), "proximo_intento_en", delay.String())
			delay = nextDelay(delay)
			continue
		}

		c.mu.Lock()
		select {
		case <-c.done:
			// Close corrió mientras se reconectaba: la conexión nueva no tiene dueño.
			c.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		c.conn = conn
		c.channel = channel
		c.healthy = true
		c.mu.Unlock()

		slog.Info("rabbitmq: reconectado")
		go c.watch(conn, channel)
		go watchReturns(channel.NotifyReturn(make(chan amqp.Return, 1)))
		return
	}
}

func nextDelay(current time.Duration) time.Duration {
	return nextBackoff(current, reconnectMaxDelay)
}

func (c *Connection) setHealthy(healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthy = healthy
}

// Healthy indica si la conexión y el canal vigentes están vivos.
func (c *Connection) Healthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// PublishAndConfirm implementa amqpChannel delegando en el canal vigente. Si el
// canal está caído y todavía no hubo reconexión, devuelve error de inmediato en
// lugar de bloquear: el llamador (Publisher) lo propaga y el worker de outbox deja
// el evento pendiente para el próximo ciclo; nunca se reintenta en loop acá.
//
// Publica con publisher confirms (dial ya activó Confirm(false) sobre este canal)
// y espera el ack/nack del broker antes de devolver: acked=false significa que el
// broker rechazó (nack) el mensaje. Un ack confirma que el broker aceptó el
// mensaje de forma durable; no confirma que haya podido enrutarlo a alguna cola,
// eso lo señala mandatory+NotifyReturn (ver watchReturns).
func (c *Connection) PublishAndConfirm(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (bool, error) {
	c.mu.RLock()
	channel := c.channel
	healthy := c.healthy
	c.mu.RUnlock()

	if !healthy || channel == nil {
		return false, errors.New("rabbitmq: sin conexión activa al broker")
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, key, mandatory, immediate, msg)
	if err != nil {
		return false, err
	}
	if confirmation == nil {
		// Solo ocurre si el canal no está en modo confirm; dial siempre lo activa,
		// así que esto sería un bug de wiring.
		return false, errors.New("rabbitmq: el canal no está en modo publisher confirm")
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return false, fmt.Errorf("rabbitmq: no se recibió el publisher confirm a tiempo: %w", err)
	}
	return acked, nil
}

// Close detiene el loop de reconexión y cierra la conexión vigente. Es seguro
// llamarlo más de una vez.
func (c *Connection) Close() error {
	c.closeOnce.Do(func() { close(c.done) })

	c.mu.Lock()
	conn := c.conn
	c.healthy = false
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	return conn.Close()
}
