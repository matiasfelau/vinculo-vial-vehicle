//go:build integration

package events

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/repository/postgres"
	"github.com/matiasfelau/vinculo-vial-vehicle/internal/service/userdeleted"
)

// Estas pruebas corren contra un RabbitMQ real (TEST_RABBITMQ_URL) porque lo que
// verifican es comportamiento del broker: x-death, TTL de la cola .retry, dead
// letter y publisher confirms. Se ejecutan con:
//
//	go test -tags integration ./internal/delivery/events/...
//
// Replican la topología que declara vinculo-vial-core (ver topology.flow): cola de
// trabajo -> exchange de retry -> .retry con TTL -> de vuelta a la cola de trabajo,
// y .dlq bindeada al exchange de dead letter por el nombre de la cola de trabajo.
const (
	integrationRetryTTLMillis = 150
	integrationMaxRetries     = 2
	integrationPrefetch       = 5
	integrationWait           = 20 * time.Second
	integrationPublishTimeout = 5 * time.Second
)

// flujo es la topología de un consumidor, con nombres únicos por prueba.
type flujo struct {
	exchange      string
	retryExchange string
	work          string
	retry         string
	dlq           string
}

// declararFlujo declara el exchange de eventos, el de retry, y las colas de
// trabajo, .retry y .dlq con sus bindings, y los borra al terminar. El exchange de
// dead letter es fijo en la plataforma (dlqExchange): se declara igual que Core
// (topic, durable) y no se borra, por si lo comparte con un broker de desarrollo.
func declararFlujo(t *testing.T, url string) (flujo, *amqp.Channel) {
	t.Helper()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("no se pudo conectar al broker de prueba: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("no se pudo abrir el canal: %v", err)
	}

	id := uuid.NewString()[:8]
	f := flujo{
		exchange:      "vvi.test." + id,
		retryExchange: "vvi.test.retry." + id,
		work:          "vehicle.test-" + id + ".q",
		retry:         "vehicle.test-" + id + ".retry",
		dlq:           "vehicle.test-" + id + ".dlq",
	}

	for _, exchange := range []string{f.exchange, f.retryExchange, dlqExchange} {
		if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			t.Fatalf("no se pudo declarar el exchange %s: %v", exchange, err)
		}
	}

	declaraciones := []struct {
		name string
		args amqp.Table
	}{
		{f.work, amqp.Table{"x-dead-letter-exchange": f.retryExchange, "x-dead-letter-routing-key": f.work}},
		{f.retry, amqp.Table{"x-message-ttl": int32(integrationRetryTTLMillis), "x-dead-letter-exchange": "", "x-dead-letter-routing-key": f.work}},
		{f.dlq, nil},
	}
	for _, q := range declaraciones {
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, q.args); err != nil {
			t.Fatalf("no se pudo declarar la cola %s: %v", q.name, err)
		}
	}
	bindings := []struct{ queue, key, exchange string }{
		{f.work, eventTypeUserDeleted, f.exchange},
		{f.retry, f.work, f.retryExchange},
		{f.dlq, f.work, dlqExchange},
	}
	for _, b := range bindings {
		if err := ch.QueueBind(b.queue, b.key, b.exchange, false, nil); err != nil {
			t.Fatalf("no se pudo bindear %s: %v", b.queue, err)
		}
	}

	t.Cleanup(func() {
		cleanup, err := conn.Channel()
		if err != nil {
			return
		}
		for _, q := range []string{f.work, f.retry, f.dlq} {
			_, _ = cleanup.QueueDelete(q, false, false, false)
		}
		_ = cleanup.ExchangeDelete(f.exchange, false, false)
		_ = cleanup.ExchangeDelete(f.retryExchange, false, false)
	})

	return f, ch
}

// iniciarConsumidor arranca el consumidor real (RunWithReconnect) sobre la cola
// de trabajo y devuelve una función que lo detiene y espera a que termine.
func iniciarConsumidor(t *testing.T, url string, f flujo, processor Processor) (detener func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	status := &ConnectionStatus{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunWithReconnect(ctx, url, f.work, integrationPrefetch, status, func(channel ConsumerChannel) *Consumer {
			deadLetters := NewAMQPDeadLetterPublisher(channel, integrationPublishTimeout)
			return NewConsumer(channel, f.work, NewMessageHandler(processor, deadLetters, f.work, integrationMaxRetries))
		})
	}()

	var once sync.Once
	detener = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(integrationWait):
				t.Error("el consumidor no terminó a tiempo")
			}
		})
	}
	t.Cleanup(detener)

	if !waitForLong(status.Connected) {
		t.Fatal("el consumidor no se conectó al broker")
	}
	return detener
}

func waitForLong(cond func() bool) bool {
	deadline := time.Now().Add(integrationWait)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func publicar(t *testing.T, ch *amqp.Channel, f flujo, body []byte) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), integrationPublishTimeout)
	defer cancel()
	err := ch.PublishWithContext(ctx, f.exchange, eventTypeUserDeleted, true, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body,
	})
	if err != nil {
		t.Fatalf("no se pudo publicar el evento: %v", err)
	}
}

// esperarEnDLQ devuelve el primer mensaje que llega a la DLQ.
func esperarEnDLQ(t *testing.T, ch *amqp.Channel, f flujo) amqp.Delivery {
	t.Helper()

	deadline := time.Now().Add(integrationWait)
	for time.Now().Before(deadline) {
		msg, ok, err := ch.Get(f.dlq, true)
		if err != nil {
			t.Fatalf("no se pudo leer la DLQ: %v", err)
		}
		if ok {
			return msg
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("el mensaje no llegó a la DLQ a tiempo")
	return amqp.Delivery{}
}

// cantidadEnCola devuelve los mensajes listos de la cola. Con el consumidor
// detenido, un mensaje sin ack vuelve a estar listo, así que 0 prueba el ack.
func cantidadEnCola(t *testing.T, ch *amqp.Channel, queue string) int {
	t.Helper()

	q, err := ch.QueueInspect(queue)
	if err != nil {
		t.Fatalf("no se pudo inspeccionar %s: %v", queue, err)
	}
	return q.Messages
}

func exigirColasVacias(t *testing.T, ch *amqp.Channel, f flujo, colas ...string) {
	t.Helper()

	for _, queue := range colas {
		if n := cantidadEnCola(t, ch, queue); n != 0 {
			t.Errorf("la cola %s tiene %d mensajes, esperaba 0", queue, n)
		}
	}
}

// removedorDeIntegracion es un caso de uso de prueba que cuenta las invocaciones,
// falla las primeras fallosRestantes con una indisponibilidad (transitoria) y
// recuerda a qué titulares atendió.
type removedorDeIntegracion struct {
	llamadas        atomic.Int32
	fallosRestantes atomic.Int32
	titulares       sync.Map
}

func (r *removedorDeIntegracion) Execute(_ context.Context, ownerID uuid.UUID) (int64, error) {
	r.llamadas.Add(1)
	if r.fallosRestantes.Add(-1) >= 0 {
		return 0, domain.ErrRepositoryUnavailable
	}
	r.titulares.Store(ownerID, true)
	return 1, nil
}

func (r *removedorDeIntegracion) atendio(ownerID uuid.UUID) bool {
	_, ok := r.titulares.Load(ownerID)
	return ok
}

func cuerpoDeBajaDe(t *testing.T, ownerID uuid.UUID) []byte {
	t.Helper()

	envelope := sobreDeBaja(t, actorDe(ownerID), payloadDeBaja())
	return cuerpoDeBaja(t, envelope)
}

func TestIntegracionUserDeletedValidoSeProcesaYSeConfirma(t *testing.T) {
	url := brokerURL(t)
	f, ch := declararFlujo(t, url)
	removedor := &removedorDeIntegracion{}
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(removedor))
	ownerID := uuid.New()

	publicar(t, ch, f, cuerpoDeBajaDe(t, ownerID))

	if !waitForLong(func() bool { return removedor.atendio(ownerID) }) {
		t.Fatal("el consumidor no procesó user.deleted")
	}
	detener()
	// Sin ack, el mensaje volvería a la cola al cerrarse el canal.
	exigirColasVacias(t, ch, f, f.work, f.retry, f.dlq)
	if n := removedor.llamadas.Load(); n != 1 {
		t.Errorf("el caso de uso corrió %d veces, esperaba 1", n)
	}
}

func TestIntegracionEnvelopeInvalidoVaALaDLQSinReintento(t *testing.T) {
	url := brokerURL(t)
	f, ch := declararFlujo(t, url)
	removedor := &removedorDeIntegracion{}
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(removedor))
	cuerpo := []byte(`{no es un envelope`)

	publicar(t, ch, f, cuerpo)

	msg := esperarEnDLQ(t, ch, f)
	if string(msg.Body) != string(cuerpo) {
		t.Errorf("la DLQ debe recibir el cuerpo original, obtuvo %q", msg.Body)
	}
	if motivo, _ := msg.Headers[headerDiscardReason].(string); motivo == "" {
		t.Error("la DLQ debía recibir x-discard-reason")
	}
	if msg.DeliveryMode != amqp.Persistent {
		t.Errorf("DeliveryMode = %d, esperaba persistente", msg.DeliveryMode)
	}
	detener()
	exigirColasVacias(t, ch, f, f.work, f.retry)
	if n := removedor.llamadas.Load(); n != 0 {
		t.Errorf("un mensaje inválido no debe llegar al caso de uso, llegó %d veces", n)
	}
}

func TestIntegracionPayloadInvalidoVaALaDLQSinReintento(t *testing.T) {
	url := brokerURL(t)
	f, ch := declararFlujo(t, url)
	removedor := &removedorDeIntegracion{}
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(removedor))
	sinFecha := cuerpoDeBaja(t, sobreDeBaja(t, actorDe(uuid.New()), map[string]any{}))

	publicar(t, ch, f, sinFecha)

	esperarEnDLQ(t, ch, f)
	detener()
	exigirColasVacias(t, ch, f, f.work, f.retry)
	if n := removedor.llamadas.Load(); n != 0 {
		t.Errorf("un payload inválido no debe llegar al caso de uso, llegó %d veces", n)
	}
}

// Con el broker real, x-death cuenta cada pasada por .retry: el mensaje se
// entrega MAX_RETRIES+1 veces y recién entonces va a la DLQ.
func TestIntegracionFalloTransitorioAgotaMaxRetriesYVaALaDLQ(t *testing.T) {
	url := brokerURL(t)
	f, ch := declararFlujo(t, url)
	removedor := &removedorDeIntegracion{}
	removedor.fallosRestantes.Store(1 << 20)
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(removedor))
	cuerpo := cuerpoDeBajaDe(t, uuid.New())

	publicar(t, ch, f, cuerpo)

	msg := esperarEnDLQ(t, ch, f)
	if string(msg.Body) != string(cuerpo) {
		t.Error("la DLQ debe recibir el cuerpo original")
	}
	detener()
	if n := removedor.llamadas.Load(); n != integrationMaxRetries+1 {
		t.Errorf("el caso de uso corrió %d veces, esperaba %d (1 + MAX_RETRIES)", n, integrationMaxRetries+1)
	}
	exigirColasVacias(t, ch, f, f.work, f.retry)
}

func TestIntegracionFalloTransitorioQueSeRecuperaSeProcesaEnElReintento(t *testing.T) {
	url := brokerURL(t)
	f, ch := declararFlujo(t, url)
	removedor := &removedorDeIntegracion{}
	removedor.fallosRestantes.Store(1)
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(removedor))
	ownerID := uuid.New()

	publicar(t, ch, f, cuerpoDeBajaDe(t, ownerID))

	if !waitForLong(func() bool { return removedor.atendio(ownerID) }) {
		t.Fatal("el mensaje no se procesó en el reintento")
	}
	detener()
	if n := removedor.llamadas.Load(); n != 2 {
		t.Errorf("el caso de uso corrió %d veces, esperaba 2 (un fallo y un reintento)", n)
	}
	exigirColasVacias(t, ch, f, f.work, f.retry, f.dlq)
}

// Cadena completa: broker real -> consumidor -> caso de uso -> PostgreSQL real.
// Una patente compartida con otro titular conserva su vínculo, y la reentrega del
// mismo evento no cambia el resultado.
func TestIntegracionUserDeletedBorraSusVehiculosEnPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL no configurada: se omite la prueba contra PostgreSQL")
	}
	url := brokerURL(t)

	db, err := postgres.Connect(dsn)
	if err != nil {
		t.Fatalf("no se pudo conectar a la base de datos de prueba: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := postgres.AutoMigrate(db); err != nil {
		t.Fatalf("no se pudo migrar el esquema: %v", err)
	}
	repo := postgres.NewVehicleRepository(db)

	baja, otro := uuid.New(), uuid.New()
	compartida := uuid.NewString()
	for _, v := range []struct {
		owner uuid.UUID
		plate string
	}{{baja, compartida}, {baja, uuid.NewString()}, {otro, compartida}, {otro, uuid.NewString()}} {
		err := repo.Create(context.Background(), &domain.Vehicle{ID: uuid.New(), OwnerID: v.owner, PatHashed: v.plate, PatAES: "cifrado", CreatedAt: time.Now()})
		if err != nil {
			t.Fatalf("no se pudo crear el vehículo: %v", err)
		}
	}
	t.Cleanup(func() { db.Exec("DELETE FROM vehicles WHERE owner_id IN (?, ?)", baja, otro) })

	f, ch := declararFlujo(t, url)
	servicio := &removedorContado{real: userdeleted.NewRemoveVehiclesService(repo)}
	detener := iniciarConsumidor(t, url, f, NewUserDeletedProcessor(servicio))
	cuerpo := cuerpoDeBajaDe(t, baja)

	publicar(t, ch, f, cuerpo)
	if !waitForLong(func() bool { return vehiculosDe(t, db, baja) == 0 }) {
		t.Fatal("los vehículos del usuario dado de baja no se borraron")
	}
	publicar(t, ch, f, cuerpo) // reentrega del mismo evento
	publicar(t, ch, f, cuerpo)
	if !waitForLong(func() bool { return servicio.llamadas.Load() == 3 }) {
		t.Fatalf("el consumidor procesó %d de las 3 entregas", servicio.llamadas.Load())
	}

	detener()
	exigirColasVacias(t, ch, f, f.work, f.retry, f.dlq)
	if n := vehiculosDe(t, db, baja); n != 0 {
		t.Errorf("quedaron %d vehículos del usuario dado de baja", n)
	}
	if n := vehiculosDe(t, db, otro); n != 2 {
		t.Errorf("el otro titular tiene %d vehículos, esperaba 2 (incluida la patente compartida)", n)
	}
}

// removedorContado delega en el caso de uso real y cuenta las invocaciones.
type removedorContado struct {
	real     userVehiclesRemover
	llamadas atomic.Int32
}

func (r *removedorContado) Execute(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	defer r.llamadas.Add(1)
	return r.real.Execute(ctx, ownerID)
}

func vehiculosDe(t *testing.T, db *gorm.DB, ownerID uuid.UUID) int64 {
	t.Helper()

	var n int64
	if err := db.Table("vehicles").Where("owner_id = ?", ownerID).Count(&n).Error; err != nil {
		t.Fatalf("no se pudo contar los vehículos: %v", err)
	}
	return n
}
