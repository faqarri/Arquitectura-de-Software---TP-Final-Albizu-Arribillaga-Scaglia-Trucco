FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0

COPY pkg/go.mod pkg/go.sum pkg/
COPY services/tiempo-real/go.mod services/tiempo-real/go.sum services/tiempo-real/
RUN cd services/tiempo-real && go mod download

COPY pkg/ pkg/
COPY services/tiempo-real/ services/tiempo-real/
RUN cd services/tiempo-real && go build -trimpath -ldflags="-s -w" -o /out/tiempo-real ./cmd/tiempo-real

FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/tiempo-real /usr/local/bin/tiempo-real
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["tiempo-real"]
