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
