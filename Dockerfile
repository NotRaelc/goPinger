FROM golang:1.23-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gopinger .

FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app

COPY --from=build /out/gopinger /app/gopinger
COPY frontend   /app/frontend
COPY migrations /app/migrations

RUN mkdir -p /app/frontend/static/images/games && chown -R app:app /app

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/gopinger"]