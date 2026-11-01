package events

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestNextDelayDuplicaYSeAcotaEnElMaximo(t *testing.T) {
	if got := nextDelay(time.Second); got != 2*time.Second {
		t.Fatalf("nextDelay(1s) = %v, esperaba 2s", got)
	}
	if got := nextDelay(20 * time.Second); got != reconnectMaxDelay {
		t.Fatalf("nextDelay(20s) = %v, esperaba el tope %v", got, reconnectMaxDelay)
	}
	if got := nextDelay(reconnectMaxDelay); got != reconnectMaxDelay {
		t.Fatalf("nextDelay(tope) = %v, esperaba el tope %v", got, reconnectMaxDelay)
	}
}

// Un broker inalcanzable no es un error de despliegue: el servicio arranca en modo
// degradado y los eventos esperan en el outbox.
func TestConnectConBrokerInalcanzableArrancaDegradado(t *testing.T) {
	conn, err := Connect("amqp://guest:guest@127.0.0.1:1/", "vvi.topic.exchange")
	if err != nil {
		t.Fatalf("un broker inalcanzable no debía hacer fallar Connect: %v", err)
	}
	defer conn.Close()

	if conn.Healthy() {
		t.Fatal("sin broker la conexión debía reportarse no saludable")
	}
	acked, err := conn.PublishAndConfirm(context.Background(), "vvi.topic.exchange", "k", true, false, amqp.Publishing{})
	if err == nil || acked {
		t.Fatalf("publicar sin conexión debía fallar de inmediato, obtuvo acked=%v err=%v", acked, err)
	}
}

func TestCloseEsIdempotente(t *testing.T) {
	conn, err := Connect("amqp://guest:guest@127.0.0.1:1/", "vvi.topic.exchange")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := conn.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

// brokerURL devuelve la URL de un RabbitMQ real o salta la prueba. No se simula el
// broker: el comportamiento de los confirms y de mandatory es del broker.
func brokerURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL no configurada: se omite prueba de integración con RabbitMQ")
	}
	return url
}

// declareExchange crea, como lo haría Core, un exchange topic durable de prueba.
func declareExchange(t *testing.T, url string) (exchange string, ch *amqp.Channel) {
	t.Helper()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("no se pudo conectar al broker de prueba: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ch, err = conn.Channel()
	if err != nil {
		t.Fatalf("no se pudo abrir el canal: %v", err)
	}

	exchange = "vvi.test." + uuid.NewString()
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("no se pudo declarar el exchange de prueba: %v", err)
	}
	t.Cleanup(func() { _ = ch.ExchangeDelete(exchange, false, false) })

	return exchange, ch
}

func TestConnectConExchangeInexistenteFalla(t *testing.T) {
	url := brokerURL(t)

	_, err := Connect(url, "vvi.test.no-existe."+uuid.NewString())

	if !errors.Is(err, errExchangeNotFound) {
		t.Fatalf("esperaba errExchangeNotFound, obtuvo %v", err)
	}
}

func TestPublishLlegaALaColaConElEnvelopeYConfirm(t *testing.T) {
	url := brokerURL(t)
	exchange, ch := declareExchange(t, url)

	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("no se pudo declarar la cola: %v", err)
	}
	if err := ch.QueueBind(queue.Name, "vehicle.pending.claimed", exchange, false, nil); err != nil {
		t.Fatalf("no se pudo bindear la cola: %v", err)
	}

	conn, err := Connect(url, exchange)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	defer conn.Close()
	if !conn.Healthy() {
		t.Fatal("con el broker disponible la conexión debía ser saludable")
	}

	event := sampleEvent(nil)
	if err := NewPublisher(conn, exchange, 5*time.Second).Publish(context.Background(), event); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	msg, ok, err := ch.Get(queue.Name, true)
	if err != nil || !ok {
		t.Fatalf("el mensaje no llegó a la cola (ok=%v err=%v)", ok, err)
	}
	if msg.DeliveryMode != amqp.Persistent {
		t.Fatalf("DeliveryMode = %d, esperaba persistente", msg.DeliveryMode)
	}
	if msg.MessageId != event.ID.String() {
		t.Fatalf("MessageId = %q, esperaba %q", msg.MessageId, event.ID)
	}
}

// Sin cola destino el broker devuelve el mensaje (mandatory) pero igual lo
// confirma: el aviso es el WARN de watchReturns, no un error del publish.
func TestPublishNoEnrutableSeConfirmaYSeLoguea(t *testing.T) {
	url := brokerURL(t)
	exchange, _ := declareExchange(t, url)

	conn, err := Connect(url, exchange)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	defer conn.Close()

	if err := NewPublisher(conn, exchange, 5*time.Second).Publish(context.Background(), sampleEvent(nil)); err != nil {
		t.Fatalf("un mensaje no enrutable igual se confirma: %v", err)
	}
}

func TestReconectaTrasLaPerdidaDeLaConexion(t *testing.T) {
	url := brokerURL(t)
	exchange, _ := declareExchange(t, url)

	conn, err := Connect(url, exchange)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	defer conn.Close()

	conn.mu.RLock()
	vigente := conn.conn
	conn.mu.RUnlock()
	_ = vigente.Close()

	deadline := time.Now().Add(10 * time.Second)
	for {
		conn.mu.RLock()
		reconectada := conn.healthy && conn.conn != vigente
		conn.mu.RUnlock()
		if reconectada {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("la conexión no se restableció a tiempo")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := NewPublisher(conn, exchange, 5*time.Second).Publish(context.Background(), sampleEvent(nil)); err != nil {
		t.Fatalf("tras reconectar el publish debía funcionar: %v", err)
	}
}
