package events

import (
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestDeathCountDevuelveCeroSinHeader(t *testing.T) {
	if count := deathCount(amqp.Table{}, testQueue); count != 0 {
		t.Fatalf("count = %d, esperaba 0", count)
	}
}

func TestDeathCountLeeElContadorDeLaColaDeTrabajo(t *testing.T) {
	headers := amqp.Table{
		"x-death": []interface{}{
			amqp.Table{"queue": testQueue, "reason": "rejected", "count": int64(3)},
			amqp.Table{"queue": testQueue + ".retry", "reason": "expired", "count": int64(3)},
		},
	}

	if count := deathCount(headers, testQueue); count != 3 {
		t.Fatalf("count = %d, esperaba 3", count)
	}
}

func TestDeathCountIgnoraEntradasDeOtrasColas(t *testing.T) {
	headers := amqp.Table{
		"x-death": []interface{}{
			amqp.Table{"queue": "otra.cola.q", "reason": "rejected", "count": int64(10)},
		},
	}

	if count := deathCount(headers, testQueue); count != 0 {
		t.Fatalf("count = %d, esperaba 0", count)
	}
}

func TestDeathCountToleraFormatoInesperado(t *testing.T) {
	for nombre, headers := range map[string]amqp.Table{
		"no es un arreglo":        {"x-death": "texto"},
		"entrada que no es tabla": {"x-death": []interface{}{"texto"}},
		"count de tipo extraño":   {"x-death": []interface{}{amqp.Table{"queue": testQueue, "count": "3"}}},
		"headers nil":             nil,
	} {
		t.Run(nombre, func(t *testing.T) {
			if count := deathCount(headers, testQueue); count != 0 {
				t.Fatalf("count = %d, esperaba 0", count)
			}
		})
	}
}

func TestDeathCountCuentaSoloLaColaDeTrabajoAunqueLaRetryTengaOtroContador(t *testing.T) {
	headers := amqp.Table{
		"x-death": []interface{}{
			amqp.Table{"queue": testQueue + ".retry", "reason": "expired", "count": int64(9)},
			amqp.Table{"queue": testQueue, "reason": "rejected", "count": int64(2)},
		},
	}

	if count := deathCount(headers, testQueue); count != 2 {
		t.Fatalf("count = %d, esperaba 2 (solo las entradas de la cola de trabajo)", count)
	}
}

func TestDeathCountAceptaContadoresInt32EInt(t *testing.T) {
	for nombre, valor := range map[string]interface{}{"int32": int32(4), "int": 4} {
		t.Run(nombre, func(t *testing.T) {
			headers := amqp.Table{"x-death": []interface{}{amqp.Table{"queue": testQueue, "count": valor}}}
			if count := deathCount(headers, testQueue); count != 4 {
				t.Fatalf("count = %d, esperaba 4", count)
			}
		})
	}
}
