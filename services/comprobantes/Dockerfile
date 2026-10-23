# Se construye desde la raíz del repositorio (ver docker-compose.yml), porque
# necesita el módulo compartido pkg/.

FROM golang:1.26-alpine AS build
WORKDIR /src
# Sin workspace: el go.mod del servicio apunta a ../../pkg con replace.
ENV GOWORK=off CGO_ENABLED=0

# Primero solo los go.mod/go.sum, para cachear la descarga de dependencias.
COPY pkg/go.mod pkg/go.sum pkg/
COPY services/comprobantes/go.mod services/comprobantes/go.sum services/comprobantes/
RUN cd services/comprobantes && go mod download

COPY pkg/ pkg/
COPY services/comprobantes/ services/comprobantes/
RUN cd services/comprobantes && go build -trimpath -ldflags="-s -w" -o /out/comprobantes ./cmd/comprobantes

FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/comprobantes /usr/local/bin/comprobantes
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["comprobantes"]
