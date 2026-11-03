package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	testBackoffMin = time.Millisecond
	testBackoffMax = 4 * time.Millisecond
	testWait       = 2 * time.Second
)

type fakeCloser struct{ closed atomic.Int32 }

func (f *fakeCloser) Close() error {
	f.closed.Add(1)
	return nil
}

func TestNextBackoffDuplicaHastaElTope(t *testing.T) {
	backoff := reconnectInitialDelay
	for i := 0; i < 10; i++ {
		backoff = nextBackoff(backoff, reconnectMaxDelay)
	}
	if backoff != reconnectMaxDelay {
		t.Fatalf("backoff = %v, esperaba que se estabilizara en el tope %v", backoff, reconnectMaxDelay)
	}
}

func TestNextBackoffNuncaSuperaElTope(t *testing.T) {
	if got := nextBackoff(reconnectMaxDelay, reconnectMaxDelay); got != reconnectMaxDelay {
		t.Fatalf("nextBackoff(max) = %v, esperaba %v", got, reconnectMaxDelay)
	}
}

func TestSleepOrDoneDevuelveTrueTrasElTiempo(t *testing.T) {
	if !sleepOrDone(context.Background(), 5*time.Millisecond) {
		t.Fatal("esperaba true al completar la espera")
	}
}

func TestSleepOrDoneDevuelveFalseAnteCancelacion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if sleepOrDone(ctx, time.Minute) {
		t.Fatal("esperaba false ante un contexto ya cancelado")
	}
}

func TestConfigureChannelAplicaQosConElPrefetchConfigurado(t *testing.T) {
	channel := &fakeConsumerChannel{}

	if err := configureChannel(channel, 42); err != nil {
		t.Fatalf("configureChannel() devolvió un error inesperado: %v", err)
	}

	if len(channel.qosCalls) != 1 {
		t.Fatalf("esperaba 1 llamada a Qos, hubo %d", len(channel.qosCalls))
	}
	if channel.qosCalls[0].prefetchCount != 42 {
		t.Errorf("prefetchCount = %d, esperaba 42", channel.qosCalls[0].prefetchCount)
	}
	if channel.qosCalls[0].global {
		t.Error("global debería ser false: el prefetch es por consumidor, no por canal compartido")
	}
	if channel.confirmCalls != 1 {
		t.Errorf("Confirm() debía invocarse una vez, se invocó %d", channel.confirmCalls)
	}
	if channel.notifyReturnCalls != 1 {
		t.Errorf("NotifyReturn() debía invocarse una vez, se invocó %d", channel.notifyReturnCalls)
	}
}

func TestConfigureChannelPropagaElErrorDeQosSinLlamarAConfirm(t *testing.T) {
	channel := &fakeConsumerChannel{qosErr: errors.New("qos no soportado")}

	if err := configureChannel(channel, 10); err == nil {
		t.Fatal("esperaba que configureChannel propagara el error de Qos")
	}
	if channel.confirmCalls != 0 {
		t.Errorf("Confirm() no debería invocarse si Qos falló, se invocó %d veces", channel.confirmCalls)
	}
}

func TestConfigureChannelPropagaElErrorDeConfirm(t *testing.T) {
	channel := &fakeConsumerChannel{confirmErr: errors.New("confirm no soportado")}

	if err := configureChannel(channel, 10); err == nil {
		t.Fatal("esperaba que configureChannel propagara el error de Confirm")
	}
}

func TestRunWithReconnectReconectaTrasPerderLaConexionYSeDetieneAlCancelar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dials atomic.Int32
	conn := &fakeCloser{}
	status := &ConnectionStatus{}

	open := func() (closer, ConsumerChannel, error) {
		n := dials.Add(1)
		channel := &fakeConsumerChannel{consumeDeliveries: make(chan amqp.Delivery)}
		if n == 1 {
			close(channel.consumeDeliveries) // primera conexión: se cae el canal
		}
		return conn, channel, nil
	}

	consumed := make(chan struct{})
	factory := func(channel ConsumerChannel) *Consumer {
		if dials.Load() == 2 {
			close(consumed)
		}
		return NewConsumer(channel, testQueue, &handlerFijo{})
	}

	done := make(chan struct{})
	go func() {
		runWithReconnect(ctx, open, testQueue, status, factory, backoffLimits{min: testBackoffMin, max: testBackoffMax})
		close(done)
	}()

	select {
	case <-consumed:
	case <-time.After(testWait):
		t.Fatal("no se reconectó tras perder el canal")
	}
	if !waitFor(status.Connected) {
		t.Error("tras reconectar el estado debía ser conectado")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(testWait):
		t.Fatal("runWithReconnect no terminó al cancelar el contexto")
	}

	if status.Connected() {
		t.Error("tras el apagado el estado debía ser desconectado")
	}
	if got := conn.closed.Load(); got != 2 {
		t.Errorf("conexiones cerradas = %d, esperaba 2 (una por cada conexión abierta)", got)
	}
}

func TestRunWithReconnectReintentaSiNoPuedeConectar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dials atomic.Int32
	status := &ConnectionStatus{}
	open := func() (closer, ConsumerChannel, error) {
		if dials.Add(1) < 3 {
			return nil, nil, errors.New("broker caído")
		}
		return &fakeCloser{}, &fakeConsumerChannel{consumeDeliveries: make(chan amqp.Delivery)}, nil
	}

	factory := func(channel ConsumerChannel) *Consumer {
		return NewConsumer(channel, testQueue, &handlerFijo{})
	}

	done := make(chan struct{})
	go func() {
		runWithReconnect(ctx, open, testQueue, status, factory, backoffLimits{min: testBackoffMin, max: testBackoffMax})
		close(done)
	}()

	if !waitFor(status.Connected) {
		t.Fatal("no se conectó tras los reintentos")
	}
	if got := dials.Load(); got != 3 {
		t.Errorf("intentos de conexión = %d, esperaba 3 (2 fallidos y 1 exitoso)", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(testWait):
		t.Fatal("runWithReconnect no terminó al cancelar el contexto")
	}
}

func TestRunWithReconnectConStatusNilNoFalla(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	open := func() (closer, ConsumerChannel, error) {
		cancel()
		return &fakeCloser{}, &fakeConsumerChannel{consumeDeliveries: make(chan amqp.Delivery)}, nil
	}
	factory := func(channel ConsumerChannel) *Consumer {
		return NewConsumer(channel, testQueue, &handlerFijo{})
	}

	runWithReconnect(ctx, open, testQueue, nil, factory, backoffLimits{min: testBackoffMin, max: testBackoffMax})
}

func TestConnectionStatusValorCeroReportaNoConectado(t *testing.T) {
	var status ConnectionStatus
	if status.Connected() {
		t.Fatal("el valor cero debía reportar no conectado")
	}
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
