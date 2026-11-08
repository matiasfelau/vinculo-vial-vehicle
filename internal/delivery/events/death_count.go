package events

import amqp "github.com/rabbitmq/amqp091-go"

const (
	headerXDeath = "x-death"
	xDeathQueue  = "queue"
	xDeathCount  = "count"
)

// deathCount lee el header x-death que RabbitMQ agrega cada vez que un
// mensaje es rechazado o expira desde una cola y devuelve cuántas veces
// ocurrió eso en la cola de trabajo indicada. Cuenta únicamente las
// entradas de esa cola: la cola .retry agrega la suya (por expiración del
// TTL) y sumarla duplicaría el conteo. Un mensaje que nunca fue rechazado
// no trae x-death: devuelve 0.
func deathCount(headers amqp.Table, workQueue string) int64 {
	deaths, ok := headers[headerXDeath].([]interface{})
	if !ok {
		return 0
	}

	var count int64
	for _, entry := range deaths {
		table, ok := entry.(amqp.Table)
		if !ok {
			continue
		}
		if entryQueue, _ := table[xDeathQueue].(string); entryQueue != workQueue {
			continue
		}
		if c := asInt64(table[xDeathCount]); c > count {
			count = c
		}
	}
	return count
}

func asInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	default:
		return 0
	}
}
