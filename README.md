# go-pkg

[![CI](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml/badge.svg)](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/adehikmatfr/go-pkg.svg)](https://pkg.go.dev/github.com/adehikmatfr/go-pkg)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A general-purpose Go utility library, in the style of `golang.org/x/...` — one module, many independent subpackages.

## Install

```bash
go get github.com/adehikmatfr/go-pkg@latest
```

Each subpackage is imported individually as needed, e.g.:

```go
import (
	"github.com/adehikmatfr/go-pkg/env"
	"github.com/adehikmatfr/go-pkg/jwt"
)
```

## Layout

Each utility lives in its own subdirectory as a separate package:

```text
go-pkg/
├── go.mod
├── env/       — read env vars (APP_ENV, bool/int helpers)
├── logger/    — global zerolog logger, split stdout/stderr by level
├── jwt/       — issue & parse HS256 JWTs (subject + role claim)
├── config/    — load per-environment YAML/JSON config (github.com/kkyr/fig)
├── tracer/    — OpenTelemetry TracerProvider (OTLP/HTTP), configurable sample ratio
├── postgres/  — pooled *sql.DB (lib/pq)
├── client/    — HTTP REST client (retry/backoff, proxy) & gRPC client (TLS-aware)
├── kafka/     — consumer-group listener + sync producer (IBM/sarama)
├── redis/     — context-aware get/set/delete wrapper (go-redis/v8)
└── parser/fiber/ — pagination request + standard JSON response envelope for Fiber
```

## Usage

Every public type that represents a "service" (as opposed to a DTO/config) is exposed as an interface, so it can be mocked in the consumer's own tests — see each package's `*_test.go` for an example of the mocking pattern.

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

## Conventions

- One folder = one package, folder name = package name.
- Only export (capitalized) what actually needs to be used from outside.
- Every package has its own `_test.go` file.
- Import from outside this module as: `github.com/adehikmatfr/go-pkg/<packagename>`.

## Adding a new package

```bash
mkdir <packagename>
# create <packagename>/<packagename>.go with `package <packagename>`
go test ./...
```

## Releasing

Versions follow [semantic versioning](https://semver.org/). To cut a new release:

```bash
go mod tidy
go test ./...
git tag vX.Y.Z
git push origin vX.Y.Z
GOPROXY=proxy.golang.org go list -m github.com/adehikmatfr/go-pkg@vX.Y.Z
```
