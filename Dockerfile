# syntax=docker/dockerfile:1.7

FROM golang:1.27.1-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN test -f web/downloads/NASWallboard.Desktop.exe

ARG VERSION=dev
ARG REPOSITORY=
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -buildid= -X main.version=${VERSION} -X main.repository=${REPOSITORY}" \
    -o /out/wallboard ./cmd/wallboard

FROM alpine:3.23 AS runtime-files
RUN apk add --no-cache ca-certificates tzdata

FROM scratch
COPY --from=runtime-files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-files /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build --chown=65532:65532 /out/wallboard /app/wallboard

USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/app/wallboard", "healthcheck", "--url", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/app/wallboard"]
CMD ["serve"]
