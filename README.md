# go-pkg

Koleksi package Go serbaguna (personal utility library), mirip gaya `golang.org/x/...` — satu module, banyak subpackage independen.

## Struktur

Setiap utility hidup di subdirektori sendiri sebagai package terpisah:

```
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
