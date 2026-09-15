# go-pkg

[![CI](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml/badge.svg)](https://github.com/adehikmatfr/go-pkg/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/adehikmatfr/go-pkg/v2.svg)](https://pkg.go.dev/github.com/adehikmatfr/go-pkg/v2)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A general-purpose Go utility library, in the style of `golang.org/x/...` — one module, many independent subpackages. Where a backend is realistically swappable (cache, SQL database, message broker), the package is split into a **port** (the interface/contract, backend-agnostic) and one or more **adapters** (concrete backend implementations of that port) — see [Ports & Adapters](#ports--adapters) below.

## Install

```bash
go get github.com/adehikmatfr/go-pkg/v2@latest
```

Each subpackage is imported individually as needed. A plain utility is imported directly; a port/adapter package is imported as the **port** (for the type) plus whichever **adapter** you're wiring in (for the constructor):

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/configuration/env"
	"github.com/adehikmatfr/go-pkg/v2/security/jwt"

	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"       // port (the type)
	"github.com/adehikmatfr/go-pkg/v2/datastore/cache/redis" // adapter (the constructor)
)
```

## Layout

Packages are grouped into category folders; each utility still lives in its own subdirectory as a separate package. A category that has a swappable backend nests one level deeper: `<category>/<port>/<adapter>/`.

```text
go-pkg/
├── go.mod
├── configuration/
│   ├── env/                — read env vars (APP_ENV, bool/int helpers)
│   ├── config/              — load per-environment YAML/JSON config (github.com/kkyr/fig)
│   └── configstack/       — load layered YAML files + env-prefix overlay (github.com/knadh/koanf)
├── observability/
│   ├── logger/              — global zerolog logger, split stdout/stderr by level
│   └── tracer/              — OpenTelemetry TracerProvider (OTLP/HTTP), configurable sample ratio
├── security/
│   ├── jwt/                 — issue & parse HS256 JWTs (subject + role claim)
│   ├── rsajwt/             — issue & verify RS256 JWTs with JWKS-style key rotation (Actor, key sets)
│   └── otp/                — RFC 6238/4226 TOTP/HOTP, numeric verification codes, TTL'd challenge store
├── datastore/
│   ├── cache/                — PORT: Cache interface + ErrNotFound
│   │   ├── redis/              ADAPTER: go-redis/v8
│   │   └── memory/              ADAPTER: process-local map (dev/tests, no server needed)
│   └── rdbms/                — PORT: shared Config (every adapter returns database/sql's own *sql.DB)
│       └── postgres/           ADAPTER: lib/pq
├── messaging/
│   └── broker/                — PORT: Publisher/Consumer interfaces + Message
│       └── kafka/               ADAPTER: IBM/sarama
├── client/
│   ├── rest/                 — HTTP REST client (retry/backoff, proxy)
│   └── grpc/                  — TLS-aware gRPC client dialer
├── parser/
│   └── fiber/                — pagination request + standard JSON response envelope for Fiber
├── ratelimit/               — PORT: token-bucket RateLimiter interface + ErrClosed
│   └── memory/                ADAPTER: sethvargo/go-limiter, in-process
├── structconvert/           — reflection-based struct-to-struct field copier (instance-based Converter)
└── i18n/                    — locale resolution + a message Bundle (text/template placeholder interpolation)
```

`client` and `parser` each split by protocol/framework, not by swappable backend — REST and gRPC are two different capabilities (not two implementations of the same one), so this is plain category nesting, not a port/adapter split.

## Ports & Adapters

Four categories split a **port** (the contract) from one or more **adapters** (concrete backends), so a consumer can swap the backend by changing one constructor call — nothing else:

| Port | Package | Adapters |
|------|---------|----------|
| Cache | `datastore/cache` | `datastore/cache/redis`, `datastore/cache/memory` |
| SQL database | `datastore/rdbms` | `datastore/rdbms/postgres` |
| Message broker | `messaging/broker` | `messaging/broker/kafka` |
| Rate limiter | `ratelimit` | `ratelimit/memory` |

Rules that keep this real instead of decorative:

- **The port package never imports a third-party client library.** `datastore/cache` and `messaging/broker` hold only interfaces, small data types, and sentinel errors — nothing that ties them to Redis, Kafka, or any other concrete technology.
- **An adapter imports its port and a concrete client library**, and its constructor returns the port type (e.g. `cache.NewClient` returns `cache.Cache`, not a Redis-specific type).
- **`datastore/rdbms` has no custom interface** — every SQL adapter already returns the standard library's `*sql.DB`, which is `database/sql`'s own port. Adding a second interface on top would only add indirection.
- **Not every package needs this split.** `jwt`/`rsajwt` (each already the one reasonable way to do its trust model), `config`/`configstack`, `otp`, `structconvert`, `i18n`, and `client/rest`/`client/grpc` (each already backend-agnostic within its own protocol — there's no second "REST implementation" to swap in) stay as plain packages — see `.assist/skills/ports-and-adapters/SKILL.md` for the criteria used to decide.

Swapping an adapter:

```go
// Redis in production...
var c cache.Cache = redis.NewClient(&redis.Config{Host: "redis", Port: 6379})

// ...an in-process map in a local dev build or a test — nothing else in the
// consuming code changes, because both sides only ever talk to cache.Cache.
var c cache.Cache = memory.NewClient()
```

## Usage

Every public type that represents a "service" (as opposed to a DTO/config) is exposed as an interface, so it can be mocked in the consumer's own tests — see `test/unit/<category>/<package>/` for an example of testing against each package's exported API.

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

### rsajwt

```go
issuer, err := rsajwt.NewIssuer(rsajwt.IssuerConfig{
	Issuer: "identity", Audience: []string{"internal"}, SigningKey: rsaPrivateKey, KeyID: "2026-01",
})
issued, err := issuer.Issue(rsajwt.Actor{ID: "user-123", Roles: []string{"admin"}})

keys, err := rsajwt.NewStaticKeySet(rsajwt.KeySet{"2026-01": &rsaPrivateKey.PublicKey})
verifier, err := rsajwt.NewVerifier(rsajwt.VerifierConfig{Issuer: "identity", Audience: "internal", Keys: keys})
actor, err := verifier.Verify(ctx, issued.Token) // actor.HasRole("admin")
```

### config

```go
var cfg MyAppConfig
err := config.NewConfig().ReadConfig(&cfg, "./config", "app") // loads app.<APP_ENV>.yaml
```

### configstack

```go
var cfg MyAppConfig
loader := configstack.New(configstack.WithEnvPrefix("APP_")) // APP_DB__DSN -> db.dsn
err := loader.Load(&cfg, "base.yaml", "production.yaml") // later files override earlier ones
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

### rdbms / postgres (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms"
	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms/postgres"
)

db, err := postgres.New(&rdbms.Config{ // db is a plain *sql.DB — database/sql's own port
	DSN:          os.Getenv("DATABASE_URL"),
	MaxOpenConns: 20,
	MaxIdleConns: 5,
})
```

### client/rest, client/grpc

```go
import "github.com/adehikmatfr/go-pkg/v2/client/rest"

result := rest.Get("https://api.example.com/users/1").
	WithRetryStrategy(rest.NewRetryAllErrors()).
	Execute()

var user User
err := result.Consume(&user)
```

```go
import ourgrpc "github.com/adehikmatfr/go-pkg/v2/client/grpc" // aliased: collides with "google.golang.org/grpc"

c := ourgrpc.NewGRPCClient(&ourgrpc.GRPCOpts{Cfg: &ourgrpc.GrpcConfig{Host: "svc", Port: 9090, TLS: true}, Tracer: tr})
conn, err := c.NewClient(ctx)
```

### broker / kafka (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/messaging/broker"
	"github.com/adehikmatfr/go-pkg/v2/messaging/broker/kafka"
)

consumer, err := kafka.New(&kafka.Config{Host: "kafka", Port: "9092", GroupName: "my-group"}, tr) // consumer is a broker.Consumer
consumer.ListenToTopic(ctx, "orders.created", func(ctx context.Context, msg broker.Message) {
	// handle msg.Value
})
defer consumer.Close()

publisher, err := kafka.NewProducer([]string{"kafka:9092"}, tr) // publisher is a broker.Publisher
err = publisher.Publish(ctx, "orders.created", payloadJSON)
```

### cache / redis / memory (port / adapters)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"
	"github.com/adehikmatfr/go-pkg/v2/datastore/cache/redis"
)

var c cache.Cache = redis.NewClient(&redis.Config{Host: "redis", Port: 6379})
err := c.SetObject(ctx, "user:1", user, 10*time.Minute)
var cached User
err = c.GetObject(ctx, "user:1", &cached) // returns cache.ErrNotFound if missing
```

### parser/fiber

```go
resp := fiberparser.NewSingleResponse[User]()
resp.CreateResponse(user, "ok", nil)
return fiberparser.ResponseJSON(c, resp)
```

### ratelimit / memory (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/ratelimit"
	"github.com/adehikmatfr/go-pkg/v2/ratelimit/memory"
)

limiter, err := memory.New(ratelimit.Config{Tokens: 5, Interval: time.Minute})
defer limiter.Close(ctx)

allowed, retryAfter, err := limiter.Take(ctx, "user:123")
if !allowed {
	// respond 429, Retry-After: retryAfter
}
```

### otp

```go
provisioner := otp.NewTOTPProvisioner(otp.TOTPParams{})
secret, err := provisioner.Provision(ctx, "my-service", "user@example.com") // secret.URI -> QR code

validator := otp.NewTOTPValidator(nil, otp.TOTPParams{})
ok, err := validator.Validate(ctx, secret.Base32Secret, userSuppliedCode)
```

### structconvert

```go
c := structconvert.New()
dto, err := structconvert.Convert[UserDTO](c, userModel) // matches fields by name
dtos, err := structconvert.ConvertSlice[UserDTO](c, userModels)
```

### i18n

```go
bundle := i18n.NewBundle("en")
bundle.AddMessages("en", map[string]string{"greeting": "Hello, {{.Name}}!"})
bundle.AddMessages("id", map[string]string{"greeting": "Halo, {{.Name}}!"})

loc := i18n.ResolveLocale(acceptLanguageHeader, storedPreference, "en")
text, err := bundle.Translate("greeting", loc, map[string]string{"Name": "Budi"})
```

## Conventions

- Packages are grouped into category folders (`configuration/`, `observability/`, `security/`, `datastore/`, `messaging/`, `client/`, `parser/`). Every category folder nests at least one leaf package folder underneath it — a category is never left flat with `.go` files directly inside it, even when it currently holds only one package (`security/jwt/`, `parser/fiber/`).
- One leaf folder = one package, folder name = package name (the category folder itself is not a package).
- If a package wraps a backend that could realistically be swapped for another (cache, SQL database, message broker), split it into a port (interface + shared types, no third-party imports) and one or more adapter subpackages (concrete backend, imports the port). See [Ports & Adapters](#ports--adapters). Don't force this split on a package with only one reasonable implementation (e.g. `jwt`) or on packages that are already distinct capabilities rather than interchangeable backends (`client/rest` vs. `client/grpc`).
- Only export (capitalized) what actually needs to be used from outside.
- Every test lives in `test/unit/<category>/<package>/` (black-box, `package <name>_test`), mirroring the source layout — no exceptions, no co-located `_test.go` next to source. See `.assist/skills/unit-test/SKILL.md` for how unexported-only behavior is still made testable from outside.
- Import from outside this module as: `github.com/adehikmatfr/go-pkg/v2/<category>/<packagename>` for a plain package, or add `/<port>` before the adapter name for a port/adapter package (`.../datastore/cache/redis`).

## Adding a new package

```bash
mkdir <category>/<packagename>   # or <category>/<port>/<adapter> for a port/adapter package
# create <category>/<packagename>/<packagename>.go with `package <packagename>`
mkdir -p test/unit/<category>/<packagename>
# create test/unit/<category>/<packagename>/<packagename>_test.go with `package <packagename>_test`
make test
```

Pick an existing category if the new package fits one; only introduce a new top-level category for something that doesn't.

## Releasing

Versions follow [semantic versioning](https://semver.org/). This module is currently on major version 2 (`go.mod`'s module path ends in `/v2`) after the v2.0.0 package reorganization. To cut a new release:

```bash
make tidy
make test-race
git tag v2.Y.Z
git push origin v2.Y.Z
GOPROXY=proxy.golang.org go list -m github.com/adehikmatfr/go-pkg/v2@v2.Y.Z
```

Bumping to a future v3 (or any Nth major) requires updating the `module` line in `go.mod` to end in `/vN` and updating every internal cross-package import path to match, per [Go's module major version conventions](https://go.dev/doc/modules/major-version).

## Makefile

A `Makefile` wraps the commands above (`make help` lists every target):

```bash
make fmt-check          # gofmt check, mirrors CI
make vet                # go vet
make lint               # golangci-lint run
make test-race          # go test ./... -race -count=1, mirrors CI
make tidy-check         # go mod tidy + fail if it changed anything, mirrors CI
make ci                 # everything above, in CI's order — run this before pushing

make mockery            # regenerate mocks in test/mock/ (from .mockery.yaml)
make gotest             # go test ./... with coverage instrumented (writes cover.out)
make gotestcoverage     # gotest + enforce the per-file coverage gate (.testcoverage.yml, threshold 70%)
make gotestcoveragereport  # gotest + render an HTML report (cover.html)
```

`make fmt` (no `-check`) and `make tidy` run the same tools without failing on a diff, for local fixups. The coverage gate is a local/pre-push check, not currently part of CI.

## Testing & Mocking

- Every test lives under `test/unit/<category>/<package>/`, mirroring the top-level layout, as black-box tests (`package <name>_test`) against each package's exported API — no co-located `_test.go` anywhere in this repo.
- [Mockery](https://vektra.github.io/mockery/) (`.mockery.yaml`) generates mocks into `test/mock/<path>/`, but only for **collaborator interfaces** a type depends on and calls (a tracer, an HTTP round-tripper) — never for the port/adapter product itself. Where a real lightweight fake already exists (`miniredis`, `httptest`), that's used instead of a mock.
- See `.assist/skills/unit-test/SKILL.md` for the full conventions (this file is written for an AI assistant working in this repo, but the rules apply the same either way).
