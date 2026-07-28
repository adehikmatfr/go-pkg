# go-pkg

[![CI](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml/badge.svg)](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/adehikmatfr/go-pkg.svg)](https://pkg.go.dev/github.com/adehikmatfr/go-pkg)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Koleksi package Go serbaguna (personal utility library), mirip gaya `golang.org/x/...` — satu module, banyak subpackage independen.

## Instalasi

```bash
go get github.com/adehikmatfr/go-pkg@latest
```

Setiap subpackage diimpor terpisah sesuai kebutuhan, misalnya:

```go
import (
	"github.com/adehikmatfr/go-pkg/env"
	"github.com/adehikmatfr/go-pkg/jwt"
)
```

## Struktur

Setiap utility hidup di subdirektori sendiri sebagai package terpisah:

```text
go-pkg/
├── go.mod
├── env/       — baca env var (APP_ENV, bool/int helpers)
├── logger/    — global zerolog logger, split stdout/stderr per level
├── jwt/       — issue & parse JWT HS256 (subject + role claim)
├── config/    — load YAML/JSON config per environment (github.com/kkyr/fig)
├── tracer/    — OpenTelemetry TracerProvider (OTLP/HTTP), sampling ratio configurable
├── postgres/  — pooled *sql.DB (lib/pq)
├── client/    — HTTP REST client (retry/backoff, proxy) & gRPC client (TLS-aware)
├── kafka/     — consumer-group listener + sync producer (IBM/sarama)
├── redis/     — context-aware get/set/delete wrapper (go-redis/v8)
└── parser/fiber/ — pagination request + standard JSON response envelope untuk Fiber
```

## Contoh pakai

Semua tipe publik yang mewakili sebuah "service" (bukan DTO/config) diekspos sebagai interface, jadi bisa di-mock di test milik konsumen — lihat `*_test.go` masing-masing package untuk contoh pola mock-nya.

### env

```go
e := env.NewEnv()
port := e.GetInt("PORT", 8080)
if e.IsProduction() {
	// ...
}
```

### logger

```go
logger.InitGlobalLogger(&logger.Config{ServiceName: "my-service", Level: zerolog.InfoLevel})
log.Info().Msg("service started") // github.com/rs/zerolog/log
```

### jwt

```go
signer, err := jwt.NewSigner(os.Getenv("JWT_SECRET"))
token, err := signer.Generate("user-123", "admin", 15*time.Minute)
claims, err := signer.Parse(token)
```

### config

```go
var cfg MyAppConfig
err := config.NewConfig().ReadConfig(&cfg, "./config", "app") // loads app.<APP_ENV>.yaml
```

### tracer

```go
tr, err := tracer.New(&tracer.Config{
	EndpointURL: "tempo:4318",
	ServiceName: "my-service",
	SampleRatio: 0.1, // sample 10% of traces in production
})
defer tr.Close()
```

### postgres

```go
db, err := postgres.New(&postgres.Config{
	DSN:          os.Getenv("DATABASE_URL"),
	MaxOpenConns: 20,
	MaxIdleConns: 5,
})
```

### client

```go
result := client.Get("https://api.example.com/users/1").
	WithRetryStrategy(client.NewRetryAllErrors()).
	Execute()

var user User
err := result.Consume(&user)
```

### kafka

```go
consumer, err := kafka.New(&kafka.Config{Host: "kafka", Port: "9092", GroupName: "my-group"}, tr)
consumer.ListenToTopic(ctx, "orders.created", func(ctx context.Context, msg *sarama.ConsumerMessage) {
	// handle msg
})
defer consumer.Close()

publisher, err := kafka.NewProducer([]string{"kafka:9092"}, tr)
err = publisher.Publish(ctx, "orders.created", payloadJSON)
```

### redis

```go
rdb := redis.NewClient(&redis.Config{Host: "redis", Port: 6379})
err := rdb.SetObject(ctx, "user:1", user, 10*time.Minute)
var cached User
err = rdb.GetObject(ctx, "user:1", &cached) // returns redis.ErrNotFound if missing
```

### parser/fiber

```go
resp := fiberparser.NewSingleResponse[User]()
resp.CreateResponse(user, "ok", nil)
return fiberparser.ResponseJSON(c, resp)
```

## Konvensi

- Satu folder = satu package, nama folder = nama package.
- Hanya export (huruf kapital) yang memang perlu dipakai dari luar.
- Setiap package punya file `_test.go` sendiri.
- Import dari luar module ini: `github.com/adehikmatfr/go-pkg/<namapackage>`.

## Menambah package baru

```bash
mkdir <namapackage>
# buat <namapackage>/<namapackage>.go dengan `package <namapackage>`
go test ./...
```

## Rilis

Versi mengikuti [semantic versioning](https://semver.org/). Untuk merilis versi baru:

```bash
go mod tidy
go test ./...
git tag vX.Y.Z
git push origin vX.Y.Z
GOPROXY=proxy.golang.org go list -m github.com/adehikmatfr/go-pkg@vX.Y.Z
```
