# vinculo-vial-vehicle

Servicio de vinculación de vehículos de la plataforma Vínculo Vial. Vincula usuarios con
patentes (US-14): el usuario escribe el dominio a mano y el sistema **no verifica la
titularidad**. Cada usuario puede tener hasta `VEHICLE_MAX_PER_USER` vehículos.

## Responsabilidades

- Vincular una patente al usuario de la sesión. Recibe un único criptograma (`payload_enc`,
  HPKE) con `{ "patente" }`; si trae `chasis`, se ignora.
- Desencriptar, validar el formato (`AAA999` o `AA999AA`), calcular el Blind Index y cifrar la
  patente en reposo.
- Aplicar el tope por usuario y la regla de unicidad `(owner_id, patente)`.
- Desvincular una patente que el usuario vuelve a escribir, o todas juntas.
- Reclamar las alertas pendientes de la patente al vincularla (US-10).
- Resolver los dueños de una patente para Alert por `/internal/resolve-owner`, en la red
  interna. No expone ninguna consulta pública de existencia de patentes.
- Sellar la patente para Audit y borrar los vehículos de un usuario dado de baja (US-11).

## Vinculación por dominio (US-14)

**Riesgo aceptado.** La patente se vincula sin verificar la titularidad, porque el Estado no
ofrece a los privados un servicio para validarla. Un tercero puede vincular la patente de otra
persona y recibir sus alertas, incluidas las pendientes; rotar vinculaciones (vincular, recibir,
desvincular, vincular otra) cae en el mismo riesgo. Se acepta porque las fotos, tomadas a 1–2 m
del vehículo, dan muy poca información del lugar. La mitigación es el tope de
`VEHICLE_MAX_PER_USER` (3 por defecto) vehículos por usuario.

**Privacidad.** Ninguna respuesta devuelve patentes: la app solo sabe cuántos vehículos tiene
vinculados el usuario, nunca cuáles. Para desvincular una, hay que volver a escribirla; quien
acceda a una sesión ajena no puede ver qué vehículos tiene esa persona. Ni la patente ni el
criptograma se loguean.

## Endpoints públicos

Pasan por Kong, que inyecta `X-User-Id`. La patente viaja siempre en `payload_enc`, cifrada con HPKE
contra la clave pública de Vehicle.

| Método y path | Body | Respuesta |
|---|---|---|
| `POST /api/v1/vehicles` | `{ "payload_enc" }` (payload `{ "patente" }`) | `201 { "count", "max" }` |
| `GET /api/v1/vehicles` | — | `200 { "count", "max" }` |
| `POST /api/v1/vehicles/unlink` | `{ "payload_enc" }` (payload `{ "patente" }`) | `200 { "count", "max" }` |
| `DELETE /api/v1/vehicles` | — | `204`: desvincula todos los vehículos del usuario |
| `GET /health`, `GET /ready`, `GET /health/deps` | — | Salud del servicio y de sus dependencias |

`count` es cuántos vehículos tiene vinculados el usuario y `max` es `VEHICLE_MAX_PER_USER`.

**Errores.** Todos llevan `{ "error": "<mensaje>", "code": "<CODE>" }`; la app mapea por `code`.

| Status | `code` | Caso |
|---|---|---|
| 400 | `INVALID_PLATE` | Criptograma inválido, cuerpo mal formado o formato de patente inválido |
| 401 | `UNAUTHORIZED` | `X-User-Id` ausente o inválido |
| 404 | `VEHICLE_NOT_FOUND` | `unlink` de una patente que el usuario no tiene vinculada (aunque la tenga otro usuario) |
| 409 | `DUPLICATE_VEHICLE` | El usuario ya tiene esa patente vinculada |
| 409 | `VEHICLE_LIMIT_REACHED` | El usuario ya tiene `VEHICLE_MAX_PER_USER` vehículos |
| 500 | `INTERNAL_ERROR` | Error no previsto |
| 503 | `DEPENDENCY_UNAVAILABLE` | Base caída o en timeout; lleva el header `Retry-After` |

Un duplicado se informa como `DUPLICATE_VEHICLE` aunque el usuario esté en el tope.

### Reglas

- **Tope y concurrencia:** el conteo y el alta van en la misma transacción, bajo un advisory lock
  por `owner_id` (`pg_advisory_xact_lock`), así que dos altas simultáneas no superan el tope. El
  lock de titular se toma antes que el de patente, y nadie los toma al revés.
- **Alta:** guarda el Blind Index y la patente cifrada con AES, y reclama las alertas pendientes de
  la patente (ver "Patentes pendientes de entrega"). Si el alta se rechaza (tope o duplicado), la
  reserva pendiente queda intacta.
- **Desvinculación:** `unlink` busca por Blind Index entre los vehículos del usuario de la sesión.
  Ambas formas borran el vínculo y liberan el lugar de inmediato, no emiten eventos y no tocan las
  patentes pendientes. Las alertas ya recibidas siguen en el buzón; las nuevas dejan de llegarle
  porque `resolve-owner` ya no lo encuentra. `DELETE /api/v1/vehicles` es la salida para quien
  vinculó una patente mal escrita y no la recuerda.
- **Otros titulares:** que otro usuario tenga la misma patente vinculada no cambia: cada uno tiene
  su fila, y si uno desvincula, el otro la conserva.

`POST /api/v1/vehicles/check` se eliminó en US-08: era un oráculo público de existencia de
patentes. Responde 404.

## Arquitectura

El servicio sigue Clean Architecture: el paquete `internal/domain` no depende de frameworks
HTTP, ORM ni de librerías de criptografía. Las dependencias externas (Gin, GORM, HPKE) se
integran únicamente en las capas de infraestructura (`repository`, `delivery`, `crypto`).

```
/cmd/api                    Punto de entrada del binario
/internal/domain            Entidades e interfaces del dominio
/internal/service/vehicle   Casos de uso: vincular, contar y desvincular
/internal/service/sweeper   Borrado periódico de patentes pendientes vencidas
/internal/repository/postgres  Persistencia (GORM/PostgreSQL)
/internal/delivery/http     Handlers HTTP (Gin)
/internal/crypto/hpke       Cifrado y desencriptación HPKE
/internal/crypto/symmetric  Cifrado AES-256-GCM at-rest
/internal/crypto/blindindex Cálculo del Blind Index (HMAC-SHA256)
/internal/secure            Utilidades de Zero-Retention
/internal/validation        Validación de formato de patente
/pkg/config                 Carga de configuración por variables de entorno
```

## Configuración

Ver [.env.example](.env.example) para el detalle de las variables de entorno requeridas.

| Variable | Descripción |
|---|---|
| `VEHICLE_MAX_PER_USER` | Vehículos que puede tener vinculados un usuario (US-14). Entero mayor o igual a 1. Por defecto 3 |

US-14 eliminó `MOCK_DNRPA_OWNER_DNI`, `IDENTITY_INTERNAL_URL`, `IDENTITY_TIMEOUT_MS`,
`IDENTITY_CB_*` e `IDENTITY_HPKE_PUBLIC_KEY`, junto con el puerto DNRPA, su mock y el cliente de
Identity. `/health/deps` ya no reporta a Identity. `INTERNAL_SERVICE_TOKEN` se queda: protege el
router interno de `resolve-owner`.

## Endpoint interno: resolución del dueño

`POST /internal/resolve-owner` lo consume solo `vinculo-vial-alert`, por la red
`vinculo-vial` y con el header `X-Internal-Service-Token`. Nunca se expone en Kong.

```
req:  { "pat_enc": "<base64>", "alert_id": "uuid" }
res:  200 { "owner_ids": ["uuid", ...], "pat_enc_ia": "<base64>", "pending_ref": "uuid",
            "pat_enc_audit": "<base64>", "audit_key_id": "<string>" }
      400 criptograma, formato de patente o alert_id inválidos · 503 base no disponible
```

- `pat_enc_ia` es la patente canónica cifrada con HPKE hacia `IA_HPKE_PUBLIC_KEY`, con
  `info = alert_id`. Se devuelve **siempre**, también cuando `owner_ids` está vacío
  (US-10): el emisor nunca se entera de si la patente tiene dueños, e IA valida la
  alerta igual.
- `pat_enc_audit` y `audit_key_id` (US-11) también se devuelven **siempre**, con o sin
  dueños y con o sin `pending_ref`. Ver "Sellado de la patente para Audit".
- `owner_ids` vacío (`[]`, nunca `null`) es un resultado válido, no un error.
- `pending_ref` solo se devuelve cuando `owner_ids` está vacío (ver "Patentes pendientes").
- La patente en claro se purga de memoria en todos los caminos. Ni la patente, ni
  `pat_enc`, ni `pat_enc_ia`, ni `pat_enc_audit`, ni el Blind Index se loguean.

### Sellado de la patente para Audit (US-11)

`pat_enc_audit` es la patente canónica, la misma con la que se calcula el Blind Index y se
genera `pat_enc_ia`, sellada con HPKE hacia la clave pública de Audit
(`AUDIT_PUBLIC_KEY`). El formato es el de `pat_enc_ia`: base64 estándar de `enc‖ct`, con la
suite DHKEM(X25519, HKDF-SHA256) / HKDF-SHA256 / AES-256-GCM en modo base.

| Campo | Valor |
|---|---|
| `info` HPKE | UTF-8 de `"vv-audit:pat:" + alert_id` |
| Texto plano | Patente canónica |
| `audit_key_id` | `AUDIT_KEY_ID`: identifica la clave pública usada, para que la apertura sepa qué clave privada aplicar si se rota |

- **Atado a la alerta y al campo:** si el criptograma se mueve a otro registro, o se
  intercambia con el del DNI (que Identity sella con `info = "vv-audit:dni:" + alert_id`),
  la apertura falla.
- **Instancia propia:** usa un cifrador independiente del de IA, construido con
  `AUDIT_PUBLIC_KEY`. Cada llamada produce un criptograma nuevo.
- **Todo o nada:** si el cifrado falla, la respuesta es un error interno (500) sin ningún
  campo parcial.
- **No se persiste:** Vehicle no guarda el criptograma ni la patente en claro. `vehicles` sigue
  con `pat_hashed` y `pat_aes`, y `pending_plates` con `plate_hmac`. Alert lo lleva a Audit por
  su outbox.
- **Audit nunca descifra:** guarda el criptograma tal como llega. La clave privada la genera
  `audit-keygen` (repo de Audit), la custodia el responsable legal fuera de línea y no la carga
  ningún servicio, Vehicle incluido.

| Variable | Descripción |
|---|---|
| `AUDIT_PUBLIC_KEY` | Clave pública X25519 de Audit, en base64 (32 bytes). Obligatoria |
| `AUDIT_KEY_ID` | Id corto del par de claves (hasta 32 caracteres, por ejemplo `2026-11`). Obligatoria |

Los dos valores tienen que ser iguales en Vehicle e Identity. Son fail-fast: sin ellos, o con
una clave que no decodifica a 32 bytes, el servicio no arranca.

## Patentes pendientes de entrega (US-10)

Cuando Alert resuelve una patente sin dueños, Vehicle la reserva en la tabla `pending_plates`
para que el primer usuario que la vincule reciba las alertas de las últimas
`PENDING_DELIVERY_HOURS` (24 h por defecto).

| Columna | Contenido |
|---|---|
| `plate_hmac` | Blind Index de la patente (el mismo HMAC de `vehicles.pat_hashed`). Clave primaria: una fila por patente |
| `pending_ref` | UUID opaco que Alert guarda junto a sus alertas. Único |
| `expires_at` | `now + PENDING_DELIVERY_HOURS`; se extiende con cada alerta nueva a esa patente |
| `created_at` | Alta de la reserva vigente |

La tabla nunca guarda la patente en claro, ni identificadores de alertas ni de emisores.

- **Sin dueños:** `resolve-owner` crea la fila o, si hay una vigente, reutiliza su `pending_ref` y
  extiende `expires_at`. Una fila vencida que el sweeper todavía no borró no se reutiliza: se
  emite un `pending_ref` nuevo.
- **Con dueños** (incluso si el único es el propio emisor, que Vehicle no conoce): no hay
  reserva. Alert descarta al emisor de `owner_ids`.
- **Concurrencia:** la consulta de dueños y la reserva ocurren en una transacción bajo un
  advisory lock por patente (`pg_advisory_xact_lock`), que también toma la vinculación. Dos
  `resolve-owner` simultáneos a la misma patente comparten un único `pending_ref`.
- **Vinculación:** al crear el vehículo, en la misma transacción y bajo el mismo lock, se borra la
  reserva de la patente. Si estaba vigente se encola `vehicle.pending.claimed` en el outbox; si había
  vencido, se borra sin evento. Si el vehículo no se llega a crear, la transacción se revierte y la reserva queda intacta (por ejemplo, vinculación duplicada o tope alcanzado). Como el `DELETE` toma el lock de la fila, dos
  vinculaciones simultáneas de la misma patente generan un único evento.
- **Vencimiento:** un sweeper borra las filas vencidas cada `SWEEPER_INTERVAL_MS`.

### Evento `vehicle.pending.claimed`

Se publica por `RABBITMQ_EXCHANGE` con la routing key `vehicle.pending.claimed` y el envelope estándar:

```json
{
  "eventId": "<id de la fila del outbox>",
  "eventType": "vehicle.pending.claimed",
  "timestamp": "RFC3339",
  "producer": "vinculo-vial-vehicle",
  "actor": "<owner_id del nuevo dueño>",
  "payload": { "pending_ref": "uuid" }
}
```

El payload no lleva la patente, el blind index ni identificadores de alertas. Alert lo consume en
`alert.vehicle-pending-claimed.q` y debe deduplicar por `eventId` (entrega at-least-once).

## Eventos: outbox y publisher (US-10)

Vehicle publica eventos por primera vez, con el mismo patrón que Alert e Identity:

- **Transactional outbox:** el evento se inserta en `outbox_events` en la misma transacción que el
  cambio que lo origina. Nunca hay un publish suelto después del commit.
- **Worker:** publica los pendientes cada `OUTBOX_WORKER_INTERVAL_MS`, hasta `OUTBOX_WORKER_BATCH_SIZE`
  por ciclo. Marca el evento como enviado solo después del *publisher confirm* del broker; si falla,
  registra el intento y lo reintenta en el próximo ciclo con el mismo `eventId`, así que la entrega
  es *at-least-once* y el consumidor debe deduplicar por `eventId`.
- **Envelope estándar:** `{ eventId, eventType, timestamp, producer, actor, payload }`, mensajes
  persistentes, `mandatory=true` y mensajes devueltos logueados (sin el cuerpo). La routing key es
  el `eventType`.
- **Topología:** la declara solo `vinculo-vial-core`. Vehicle verifica con `ExchangeDeclarePassive` que
  `RABBITMQ_EXCHANGE` exista, al arrancar y al reconectar; si no existe, el arranque falla.
- **Broker caído:** no es un error de despliegue. El servicio arranca en modo degradado, reconecta
  en background con backoff (1 s a 30 s) y los eventos esperan en el outbox. Ninguna operación de
  negocio depende del broker.
- **Apagado ordenado:** se drenan los servidores HTTP y luego se detiene el worker, esperando al lote
  en curso (hasta 5 s).

```
/internal/delivery/events    Conexión AMQP con reconexión, publisher, consumidor y envelopes
/internal/service/outbox      Worker de publicación
/internal/service/userdeleted Caso de uso de la baja de cuenta
```

## Baja de cuenta: consumidor de `user.deleted` (US-11)

Cuando un usuario da de baja su cuenta, Identity publica `user.deleted` (`actor` = el usuario
dado de baja, payload `{ "deleted_at": "RFC3339" }`). Vehicle lo consume en
`vehicle.user-deleted.q` (`USER_DELETED_QUEUE`) y, en una transacción, ejecuta
`DELETE FROM vehicles WHERE owner_id = <actor>`.

- **Alcance:** solo las filas del usuario. Si otro titular vinculó la misma patente conserva su
  vínculo (`UNIQUE(owner_id, pat_hashed)`: son filas distintas). `pending_plates` no se toca.
- **Idempotente:** repetir la entrega no encuentra filas y termina sin error, así que no hace
  falta deduplicar por `eventId`.
- **Validación:** el `eventType` debe ser `user.deleted`, el `actor` un UUID no nulo y
  `deleted_at` una fecha RFC3339. Un mensaje que no cumple es inválido.
- **Logs:** solo `eventId` y la cantidad de vehículos borrados; nunca el usuario ni patentes.

### Resiliencia del consumidor

Es el primer consumidor del servicio y sigue el patrón de Alert:

- **Ack manual** después de procesar con éxito, y `prefetch` (`basic.qos`) en `RABBITMQ_PREFETCH`.
- **Fallo transitorio** (base caída o en timeout): `nack` sin requeue. La cola de trabajo lo
  deriva a `.retry`, que lo devuelve al vencer su TTL. Se cuenta con `x-death` y, superados
  `MAX_RETRIES` reintentos, se publica en la DLQ.
- **Mensaje inválido** (envelope ilegible, `eventType`, `actor` o payload fuera del contrato) o
  **panic** del procesador: directo a la DLQ, sin reintento.
- **DLQ:** se publica en `vvi.dlq.exchange` con la routing key igual al **nombre de la cola de
  trabajo** (nunca la del evento), persistente, `mandatory=true` y con *publisher confirm*; el
  original solo recibe `ack` después del confirm. Si la DLQ no confirma, vuelve al ciclo de retry.
  El motivo del descarte va en el header `x-discard-reason`, sin contenido del mensaje.
- **Reconexión:** el consumidor tiene su propia conexión y reconecta con backoff (1 s a 30 s). Un
  broker caído no afecta a las rutas HTTP.
- **Apagado ordenado:** al recibir `SIGTERM` deja de consumir y termina el mensaje en curso sin
  cancelarle el contexto (si no, la consulta fallaría y gastaría un reintento); lo no procesado
  vuelve a la cola. El apagado espera hasta 5 s, en conjunto con el worker de outbox.
- **`GET /health/deps`:** reporta `deps.user_deleted_consumer` (`up` o `down`). Si está caído, 503
  `degraded`. No afecta a `/ready`.
- **Topología:** la declara solo `vinculo-vial-core` (la cola, su `.retry` y su `.dlq`).

| Variable | Default | Descripción |
|---|---|---|
| `USER_DELETED_QUEUE` | `vehicle.user-deleted.q` | Cola de trabajo; debe coincidir con Core |
| `RABBITMQ_PREFETCH` | `10` | Mensajes sin ack en vuelo (1 a 65535) |
| `MAX_RETRIES` | `5` | Reintentos antes de la DLQ (≥ 0) |

## Comandos

```
go run ./cmd/api
go test ./...
# con un RabbitMQ disponible se ejecutan también las pruebas de integración del publisher:
# TEST_RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test ./...
# pruebas de integración del consumidor contra un RabbitMQ real (build tag `integration`;
# con DATABASE_URL se prueba además la cadena completa hasta PostgreSQL):
# TEST_RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test -tags integration ./internal/delivery/events/...
CGO_ENABLED=0 go build -o vehicle ./cmd/api
docker compose up --build
```

## Nomenclatura de infraestructura Docker

`docker-compose.yml` fija explícitamente los nombres de contenedor, red y volumen para
garantizar resolución DNS consistente entre microservicios sobre la red compartida de la
plataforma:

- **Contenedor principal**: `vinculo-vial-vehicle`, fijado con `container_name`. Es el
  único nombre que otros microservicios necesitan resolver.
- **Contenedor secundario (base de datos)**: `vinculo-vial-vehicle-db`.
- **Red compartida**: `vinculo-vial`, externa (`external: true`); la crea el ecosistema,
  no este compose. Solo el contenedor principal se conecta acá.
- **Red interna**: `vehicle-internal`, local a este servicio; aísla el tráfico hacia la
  base de datos, que no debe ser alcanzable desde otros microservicios.
- **Volumen persistente**: `vinculo-vial-vehicle-data`.

Sin `container_name` explícito, Compose deriva el nombre del contenedor del directorio del
proyecto más un sufijo numérico (`vinculo-vial-vehicle-vinculo-vial-vehicle-1`), que no
coincide con el nombre que el resto de la plataforma espera resolver por DNS sobre la red
compartida.

## CI

`.github/workflows/ci.yml` corre en todo `pull_request` hacia `dev` o `main` (y en los push a esas ramas). Tiene dos jobs:

- **`build-and-test`**: compila, `go vet`, y corre las pruebas con cobertura contra un PostgreSQL y un RabbitMQ reales (servicios del job). Une el perfil de las pruebas unitarias con el de `-tags=integration` en un único reporte (`gocovmerge`) y lo convierte a Cobertura XML (`gocover-cobertura`).
  - **Cobertura total**: va a `$GITHUB_STEP_SUMMARY` y al artefacto `cobertura`. No corta el job. Cuando todos los repos lleguen al 85 % (v0.14), el gate pasa a ser sobre el total.
  - **Gate sobre el código nuevo**: `diff-cover` exige al menos 85 % de cobertura en las líneas que el PR cambia respecto de la rama base del PR (`github.base_ref`, pasada por variable de entorno `BASE_REF`). Por eso el checkout usa `fetch-depth: 0`.
- **`security`**: `govulncheck` (falla ante vulnerabilidades alcanzables), Trivy `fs` (HIGH y CRITICAL con corrección disponible, secretos y configuración) y Semgrep. Semgrep es solo informativo por ahora: deja el resultado en el resumen y en el artefacto `semgrep`, y pasa a gate en v0.14.

**Exclusiones de cobertura:** ninguna. El repo no tiene código generado y no se excluye código escrito a mano.

**Excepciones de Trivy:** solo en `.trivyignore`, cada una con un comentario que diga por qué y hasta cuándo.

**Higiene:** el workflow tiene `permissions: contents: read`, las acciones de terceros están fijadas por SHA de commit (con la versión en un comentario) y `.github/dependabot.yml` revisa semanalmente `gomod`, `github-actions` y `docker`.

### Correr los chequeos en local

Con un PostgreSQL y un RabbitMQ levantados (por ejemplo con `docker run`) y las variables apuntando a ellos:

```bash
export DATABASE_URL='postgres://vehicle:vehicle@localhost:5432/vehicle?sslmode=disable'
export TEST_RABBITMQ_URL='amqp://guest:guest@localhost:5672/'

# herramientas (las mismas versiones que el workflow, ver el bloque env de ci.yml)
go install github.com/wadey/gocovmerge@b5bfa59ec0ad
go install github.com/boumenot/gocover-cobertura@v1.5.0
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
pip install diff-cover==11.0.1

# cobertura: unitarias + integración, unidas
go test ./... -covermode=atomic -coverpkg=./... -coverprofile=coverage-unit.out
go test ./... -tags=integration -covermode=atomic -coverpkg=./... -coverprofile=coverage-integration.out
gocovmerge coverage-unit.out coverage-integration.out > coverage.out
gocover-cobertura < coverage.out > coverage.xml
go tool cover -func=coverage.out | tail -1          # cobertura total

# gate sobre el código nuevo (necesita la rama base actualizada: git fetch origin dev)
diff-cover coverage.xml --compare-branch=origin/dev --fail-under=85

# seguridad
govulncheck ./...
trivy fs --scanners vuln,secret,misconfig --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 .
semgrep scan --config p/default --metrics=off
```

Si no hay PostgreSQL ni RabbitMQ, las pruebas que los necesitan se omiten solas y la cobertura que se mide es menor que la de CI.
