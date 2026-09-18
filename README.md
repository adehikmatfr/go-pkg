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
│   ├── rdbms/                — PORT: shared Config (every adapter returns database/sql's own *sql.DB)
│   │   └── postgres/           ADAPTER: lib/pq
│   └── storage/              — PORT: Storage interface (opaque ref, signed URL, KindPolicy, MIME sniffing) + sentinel errors
│       └── gcs/                 ADAPTER: gocloud.dev/blob + gcsblob (Google Cloud Storage)
├── messaging/
│   ├── broker/                — PORT: Publisher/Consumer interfaces + Message
│   │   └── kafka/               ADAPTER: IBM/sarama
│   └── outbox/                — PORT: transactional-outbox Store + Event + a vendor-agnostic Relay (drains through messaging/broker.Publisher)
│       ├── postgres/            ADAPTER: lib/pq-compatible *sql.DB, claims rows via FOR UPDATE SKIP LOCKED
│       └── memory/              ADAPTER: in-memory, for tests/dev
├── client/
│   ├── rest/                 — HTTP REST client (retry/backoff, proxy)
│   ├── grpc/                  — TLS-aware gRPC client dialer
│   └── resilience/            — builds a resilient http.RoundTripper (retry, circuit breaker, per-attempt timeout) via failsafe-go; plug it into any *http.Client
├── parser/
│   └── fiber/                — pagination request + standard JSON response envelope for Fiber
├── ratelimit/               — PORT: token-bucket RateLimiter interface + ErrClosed
│   └── memory/                ADAPTER: sethvargo/go-limiter, in-process
├── notification/
│   ├── email/                — PORT: EmailSender interface + EmailMessage/Address + sentinel errors
│   │   ├── smtp/                ADAPTER: wneessen/go-mail
│   │   └── memory/              ADAPTER: in-memory fake, records sends for tests
│   ├── sms/                  — PORT: SmsSender interface + SmsMessage + sentinel errors
│   │   └── twilio/              ADAPTER: twilio/twilio-go
│   ├── push/                 — PORT: PushSender interface + PushMessage + sentinel errors
│   │   ├── fcm/                 ADAPTER: firebase.google.com/go/v4 (FCM)
│   │   └── memory/              ADAPTER: in-memory fake, records sends for tests
│   └── dispatch/             — orchestrates email/sms/push: resolves preferences, renders content, fans out, records a delivery log. Plain package (no port/adapter split)
├── featureflag/             — PORT: FlagEvaluator interface (fail-safe Bool/StringFlag) + EvaluationContext
│   └── openfeature/            ADAPTER: open-feature/go-sdk (file-backed provider included, any other OpenFeature provider works too)
├── audit/                   — PORT: AuditStore interface + AuditEvent + hash-chain helpers (tamper-evident, append-only)
│   └── memory/                 ADAPTER: in-memory, concurrency-safe, for tests/dev
├── scheduler/
│   ├── cron/                 — PORT: recurring-job Cron interface + Locker/Unlocker + a shared crontab Schedule parser
│   │   ├── gocron/              ADAPTER: go-co-op/gocron/v2 (production, real wall-clock ticking)
│   │   └── memory/              ADAPTER: deterministic, clock-driven (tests/dev, no goroutine)
│   └── queue/                — PORT: durable background-job JobEnqueuer/HandlerRegistry interfaces + Job + SQLTx seam
│       ├── river/                ADAPTER: riverqueue/river over database/sql (production, durable/retryable)
│       └── memory/               ADAPTER: in-memory, records sends for tests
├── structconvert/           — reflection-based struct-to-struct field copier (instance-based Converter)
├── i18n/                    — locale resolution + a message Bundle (text/template placeholder interpolation)
└── apperr/                  — error taxonomy: Registry maps domain sentinels to a stable Code + HTTP/gRPC status, without changing how domain code returns errors
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
| File/object storage | `datastore/storage` | `datastore/storage/gcs` |
| Email notification | `notification/email` | `notification/email/smtp`, `notification/email/memory` |
| SMS notification | `notification/sms` | `notification/sms/twilio` |
| Push notification | `notification/push` | `notification/push/fcm`, `notification/push/memory` |
| Feature flag | `featureflag` | `featureflag/openfeature` |
| Audit trail | `audit` | `audit/memory` |
| Recurring cron scheduling | `scheduler/cron` | `scheduler/cron/gocron`, `scheduler/cron/memory` |
| Durable job queue | `scheduler/queue` | `scheduler/queue/river`, `scheduler/queue/memory` |
| Transactional outbox | `messaging/outbox` | `messaging/outbox/postgres`, `messaging/outbox/memory` |

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

### client/resilience

Retry (exponential backoff), a consecutive-failure circuit breaker, and a per-attempt timeout for outbound HTTP — plug the resulting `http.RoundTripper` into any `*http.Client`:

```go
import "github.com/adehikmatfr/go-pkg/v2/client/resilience"

rt, err := resilience.New(nil, resilience.Config{ // nil base uses http.DefaultTransport
	MaxRetries:       3,
	FailureThreshold: 5,
	Timeout:          10 * time.Second,
})
httpClient := &http.Client{Transport: rt}

resp, err := httpClient.Get("https://api.partner.example.com/v1/orders")
if resilience.IsCircuitOpen(err) {
	// fail fast — the partner has been unhealthy for FailureThreshold consecutive calls
}
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

### storage / gcs (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/datastore/storage"
	"github.com/adehikmatfr/go-pkg/v2/datastore/storage/gcs"
)

fs, err := gcs.NewClient(ctx, gcs.Config{BucketName: "my-bucket"}, storage.Options{
	Kinds: []storage.KindPolicy{
		{Name: "avatar", AllowedContentTypes: []string{"image/png", "image/jpeg"}, MaxSize: 256 << 10},
	},
})
defer fs.Close()

intent, err := fs.CreateUploadIntent(ctx, "avatar", "image/png", 0)
// client PUTs bytes to intent.SignedPutURL, then:
obj, err := fs.ConfirmUpload(ctx, intent.StorageRef) // sniffs real bytes, never trusts the declared type
url, err := fs.GetDownloadURL(ctx, obj.StorageRef, 15*time.Minute)
```

### email / smtp / memory (port / adapters)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/email/smtp"
)

sender, err := smtp.NewClient(smtp.Config{
	Host:        "smtp.example.com",
	Port:        587,
	Auth:        smtp.AuthPlain,
	Username:    "smtp-user",
	Password:    "smtp-pass",
	DefaultFrom: email.Address{Name: "MyApp", Email: "no-reply@example.com"},
})

msg := email.EmailMessage{
	To:             []email.Address{{Email: "ada@example.com"}},
	Subject:        "Welcome",
	TextBody:       "Welcome aboard.",
	IdempotencyKey: "signup-evt-7f3c", // stable per logical message; sent as Message-ID
}
err = sender.Send(ctx, msg)
```

Swap in `notification/email/memory` for tests — `memory.New()` returns a `*Sender` that also satisfies `email.EmailSender`, and records every send for assertion (`Sent()`, `Count()`, `Last()`, `FailWith` to inject a transport error).

### sms / twilio (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
	"github.com/adehikmatfr/go-pkg/v2/notification/sms/twilio"
)

sender, err := twilio.NewClient(twilio.Config{
	AccountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
	AuthToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
	FromNumber: "+15017122661",
})

id, err := sender.Send(ctx, sms.SmsMessage{
	To:             "+15558675310",
	Body:           "Your code is 123456",
	IdempotencyKey: "login-otp:" + sessionID,
})
// id is the provider's message SID (e.g. "SM...") on success.
```

### push / fcm / memory (port / adapters)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/push/fcm"
)

sender, err := fcm.NewClient(ctx, fcm.Config{ProjectID: "my-project"},
	option.WithCredentialsFile("service-account.json"),
)

id, err := sender.Send(ctx, push.PushMessage{
	Token:          deviceToken,
	Title:          "Trade filled",
	Body:           "Your AAPL order executed",
	Priority:       push.PriorityHigh,
	IdempotencyKey: eventID, // maps to the Android collapse key + apns-collapse-id
})
```

Swap in `notification/push/memory` for tests — `memory.New()` returns a `*Sender` that also satisfies `push.PushSender`, recording every send (`Sent()`, `Len()`, `Reset()`, `IDFunc`/`Err` to script the result).

### notification/dispatch (plain package)

Fans one logical notification out to every channel a recipient has enabled, wiring the `email`/`sms`/`push` senders above. The caller supplies a `Renderer` (template lookup), a `PreferenceResolver` (which channels the recipient wants) and a `DeliveryLog` (append-only audit trail + idempotency store) — `dispatch` has no opinion on how those are persisted:

```go
import "github.com/adehikmatfr/go-pkg/v2/notification/dispatch"

d, err := dispatch.NewDispatcher(
	dispatch.Senders{Email: emailSender, SMS: smsSender, Push: pushSender},
	myPreferenceResolver,
	myRenderer,
	myDeliveryLog,
)

result, err := d.Dispatch(ctx, dispatch.NotificationRequest{
	Recipient:      dispatch.Recipient{ID: "user-1", Email: "user@example.com", Phone: "+15558675310"},
	TemplateKey:    "order.filled",
	Data:           map[string]any{"symbol": "AAPL"},
	IdempotencyKey: "order-filled:" + orderID,
})
// One channel failing does not stop the others — result.Delivered/Failed/Skipped
// report the outcome per channel, and err joins every channel failure.
// Suppressed is true, err is nil, when (Recipient.ID, IdempotencyKey) was already delivered.
```

### featureflag / openfeature (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/featureflag"
	"github.com/adehikmatfr/go-pkg/v2/featureflag/openfeature"
)

evaluator, err := openfeature.NewFileBackedFlags(openfeature.Config{}, "flags.json")
defer evaluator.Close(ctx)

// Flags act as kill-switches: pass the conservative value as def. Any
// backend error, timeout, or panic returns def — BoolFlag/StringFlag never
// return an error or panic.
if evaluator.BoolFlag(ctx, "trading.killswitch", false, featureflag.EvaluationContext{
	ActorID: userID,
	Tier:    "gold",
}) {
	return ErrTradingSuspended
}
```

Wire in any other `openfeature.FeatureProvider` (LaunchDarkly, Flagsmith, GO Feature Flag, ...) via `openfeature.New(openfeature.Config{Provider: yourProvider})` instead of the file-backed one.

### audit / memory (port / adapter)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/audit"
	"github.com/adehikmatfr/go-pkg/v2/audit/memory"
)

store := memory.New()

rowHash, err := store.Append(ctx, store.Tail(), audit.AuditEvent{
	ID:     eventID, // idempotency key: re-appending the same ID is a no-op
	Source: "/orders",
	Type:   "tech.example.order.placed",
	Data:   map[string]any{"orderRef": orderID}, // references/hashes only, never PII or secrets
})

// VerifyChain recomputes every row hash and detects a later edit, reorder,
// or deletion — pass nil to verify the store's own current chain.
err = store.VerifyChain(ctx, nil)
```

### scheduler/cron / gocron / memory (port / adapters)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron/gocron"
)

c, err := gocron.New(gocron.Config{Locker: cron.NewInMemoryLocker()})
err = c.Register("*/5 * * * *", "settlement.reconcile", func(ctx context.Context) error {
	return reconcileSettlements(ctx)
})
err = c.Start(ctx)
defer c.Stop(ctx)
```

Swap in `scheduler/cron/memory` for tests — `memory.New(clock)` is driven by an injected `Clock` and an explicit `Advance(ctx)` call instead of wall-clock timers, so recurring logic is deterministic and fast.

### scheduler/queue / river / memory (port / adapters)

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue/river"
)

enqueuer, err := river.New(river.Config{DB: db, Queues: map[string]int{"default": 10}})
err = enqueuer.RegisterHandler("email_send", func(ctx context.Context, raw []byte) error {
	var args EmailSendArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return err
	}
	return sendEmail(ctx, args)
})

// EnqueueTx makes the job durable iff the surrounding transaction commits.
tx, _ := db.BeginTx(ctx, nil)
_, err = enqueuer.EnqueueTx(ctx, queue.NewSQLTx(tx), queue.NewJob(
	EmailSendArgs{To: "user@example.com"},
	queue.WithIdempotencyKey("welcome-email:"+userID),
))
// ... other writes on tx ...
tx.Commit() // job becomes durable here; rollback discards it
```

Swap in `scheduler/queue/memory` for tests — `memory.New()` records every enqueue (`Jobs()`, `VisibleJobs()`) and `memory.NewTx()` is a `queue.Tx` that fires commit/rollback callbacks without a database, so `EnqueueTx`'s atomic-visibility contract is testable end-to-end.

### messaging/outbox / postgres / memory (port / adapters)

The transactional outbox pattern: `Add` writes the event row in the same `*sql.Tx` as the business change, so "the change happened" and "the event was recorded" can never disagree. `Relay.Drain` then delivers pending rows through a `messaging/broker.Publisher`, with retry backoff and a dead-letter ceiling — call `Drain` from whatever recurring driver you already have (a ticker, `scheduler/cron`, a cron job); the package owns no scheduling of its own.

```go
import (
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox/postgres"
)

store, err := postgres.New(postgres.Config{DB: db}) // provision the table yourself — see the package doc comment for the DDL

tx, _ := db.BeginTx(ctx, nil)
err = store.Add(ctx, outbox.NewSQLTx(tx), outbox.Event{
	ID:      eventID,
	Topic:   "orders.placed",
	Payload: payload,
})
// ... other writes on tx ...
tx.Commit() // event becomes durable here; rollback discards it

relay, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: kafkaPublisher})
result, err := relay.Drain(ctx, 100) // one batch; call this from your own cron/ticker
```

Swap in `messaging/outbox/memory` for tests — `memory.New()` records every added event and `memory.NewTx()` is an `outbox.Tx` that fires commit/rollback callbacks without a database, so `Relay.Drain`'s claim/retry/dead-letter logic is testable end-to-end.

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

### apperr

```go
var ErrUserNotFound = errors.New("user not found") // domain code returns this unchanged

reg := apperr.NewRegistry().
	Register(ErrUserNotFound, apperr.CodeNotFound)

// at the boundary (HTTP handler, gRPC interceptor):
resolved := reg.Resolve(err) // fails closed to apperr.CodeInternal if unregistered
w.WriteHeader(apperr.HTTPStatus(resolved.Code))

// optional: localize resolved.Key/resolved.Args with i18n.Bundle
msg, err := bundle.Translate(resolved.Key, loc, resolved.Args) // e.g. "error.not_found"
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
