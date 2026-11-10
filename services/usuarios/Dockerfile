FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0

COPY pkg/go.mod pkg/go.sum pkg/
COPY services/usuarios/go.mod services/usuarios/go.sum services/usuarios/
RUN cd services/usuarios && go mod download

COPY pkg/ pkg/
COPY services/usuarios/ services/usuarios/
RUN cd services/usuarios && go build -trimpath -ldflags="-s -w" -o /out/usuarios ./cmd/usuarios

FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/usuarios /usr/local/bin/usuarios
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["usuarios"]
