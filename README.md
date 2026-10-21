# vinculo-vial-vehicle

Servicio de vinculación de vehículos de la plataforma Vínculo Vial. Expone el alta de
vehículos asociados a un usuario, validando la propiedad del vehículo contra el servicio
Identity antes de persistir la vinculación.

## Responsabilidades

- Recibir un único criptograma (`payload_enc`, HPKE) con los datos del vehículo
  escaneados del QR (patente, chasis).
- Consultar la titularidad del vehículo contra el registro automotor (DNRPA) a partir
  de la patente, obteniendo el DNI del titular legal.
- Cifrar ese DNI con la clave pública HPKE de Identity y verificar la propiedad contra
  `vinculo-vial-identity` por red interna.
- Desencriptar, validar, calcular el Blind Index y cifrar la patente en reposo.
- Persistir la vinculación `(owner_id, patente)` respetando la regla de unicidad compuesta.

## Arquitectura

El servicio sigue Clean Architecture: el paquete `internal/domain` no depende de frameworks
HTTP, ORM ni de librerías de criptografía. Las dependencias externas (Gin, GORM, HPKE) se
integran únicamente en las capas de infraestructura (`repository`, `delivery`, `client`,
`crypto`).

```
/cmd/api                    Punto de entrada del binario
/internal/domain            Entidades e interfaces del dominio
/internal/service/vehicle   Caso de uso de vinculación de vehículo
/internal/service/sweeper   Borrado periódico de patentes pendientes vencidas
/internal/repository/postgres  Persistencia (GORM/PostgreSQL)
/internal/delivery/http     Handlers HTTP (Gin)
/internal/client/identity   Cliente hacia el servicio Identity
/internal/client/dnrpa      Adaptador (mock) de consulta al registro automotor
/internal/crypto/hpke       Cifrado y desencriptación HPKE
/internal/crypto/symmetric  Cifrado AES-256-GCM at-rest
/internal/crypto/blindindex Cálculo del Blind Index (HMAC-SHA256)
/internal/secure            Utilidades de Zero-Retention
/internal/validation        Validación de formato de patente
/pkg/config                 Carga de configuración por variables de entorno
```

## Configuración

Ver [.env.example](.env.example) para el detalle de las variables de entorno requeridas.

## Endpoint interno: resolución del dueño

`POST /internal/resolve-owner` lo consume solo `vinculo-vial-alert`, por la red
`vinculo-vial` y con el header `X-Internal-Service-Token`. Nunca se expone en Kong.

```
req:  { "pat_enc": "<base64>", "alert_id": "uuid" }
res:  200 { "owner_ids": ["uuid", ...], "pat_enc_ia": "<base64>", "pending_ref": "uuid" }
      400 criptograma, formato de patente o alert_id inválidos · 503 base no disponible
```

- `pat_enc_ia` es la patente canónica cifrada con HPKE hacia `IA_HPKE_PUBLIC_KEY`, con
  `info = alert_id`. Se devuelve **siempre**, también cuando `owner_ids` está vacío
  (US-10): el emisor nunca se entera de si la patente tiene dueños, e IA valida la
  alerta igual.
- `owner_ids` vacío (`[]`, nunca `null`) es un resultado válido, no un error.
- `pending_ref` solo se devuelve cuando `owner_ids` está vacío (ver "Patentes pendientes").
- La patente en claro se purga de memoria en todos los caminos. Ni la patente, ni
  `pat_enc`, ni `pat_enc_ia`, ni el Blind Index se loguean.

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
  advisory lock por patente (`pg_advisory_xact_lock`), que también tomará la vinculación. Dos
  `resolve-owner` simultáneos a la misma patente comparten un único `pending_ref`.
- **Vencimiento:** un sweeper borra las filas vencidas cada `SWEEPER_INTERVAL_MS`.

## Comandos

```
go run ./cmd/api
go test ./...
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
