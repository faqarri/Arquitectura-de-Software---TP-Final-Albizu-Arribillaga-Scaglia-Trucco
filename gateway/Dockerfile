FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0

COPY pkg/go.mod pkg/go.sum pkg/
COPY gateway/go.mod gateway/go.sum gateway/
RUN cd gateway && go mod download

COPY pkg/ pkg/
COPY gateway/ gateway/
RUN cd gateway && go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/gateway /usr/local/bin/gateway
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["gateway"]
