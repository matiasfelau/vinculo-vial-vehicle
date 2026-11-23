## Etapa de compilación
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/vehicle ./cmd/api

## Etapa final: imagen mínima sin shell ni herramientas adicionales
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /build/vehicle /vehicle

# Usuario sin privilegios (UID numérico: la imagen scratch no tiene /etc/passwd)
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/vehicle"]
