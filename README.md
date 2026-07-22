# tsx-tracker

A Go service that tracks companies listed on the Toronto Stock Exchange
(TSX) in a PostgreSQL database, keeps their data fresh, and exposes it
over gRPC. Every 24 hours it syncs the full TSX symbol list (adding new
listings, removing delisted companies) and refreshes a random subset of
companies with complete profile data.

## Architecture

```
cmd/server/main.go        wires everything together, starts the gRPC server
internal/config           env-var configuration (DB, refresh cadence, API key)
internal/db               Postgres repository (schema, upsert, query, pagination)
internal/provider         Financial Modeling Prep API client (symbol list + profiles)
internal/refresher        background loop syncing symbols + refreshing random subsets
internal/grpcserver       gRPC service implementation
proto/tsx/v1/tsx.proto    gRPC API definition
gen/tsx/v1                generated protobuf/gRPC Go code (run `make proto` first)
migrations/0001_init.sql  Postgres schema (embedded in internal/db/)
```

### Why PostgreSQL

The task allows Cassandra, MongoDB, or Postgres. Company records here are a
fixed, relational schema (symbol, name, CEO, sector, financial fields) with
two access patterns: exact-match lookup by primary key (symbol) and
filtered/paginated scans (by sector). That's a textbook relational
workload — Postgres gives strong consistency, indexed lookups, and simple
upserts (`ON CONFLICT`) with far less operational overhead than running
Cassandra (built for massive write throughput across many nodes) or
MongoDB (best when the schema is genuinely variable/nested) for this use
case. The DB connection is fully configurable via env vars (`DB_HOST`,
`DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`).

### Where the data comes from

[Financial Modeling Prep](https://site.financialmodelingprep.com/) has a
free-tier REST API with:
- an exchange symbol list endpoint (`/api/v3/symbol/TSX`) — symbol, name, price
- a company profile endpoint (`/api/v3/profile/{symbols}`) — CEO, sector,
  industry, description, website, headquarters, employee count, market cap

Both are usable with a free API key (sign up, no payment required).

**Free-tier tradeoff:** the TSX lists well over 1,500 companies, and free
API tiers rate-limit requests per day. Fetching full fundamentals for the
entire TSX universe in a single cycle isn't realistic on a free plan. The
service handles this by refreshing a random subset (`DAILY_REFRESH_COUNT`,
default 50) per cycle, in small upstream batches (`PROFILE_BATCH_SIZE`,
default 3). Over multiple days the random selection ensures all companies
get refreshed. Every cycle the full TSX symbol list is synced — new
listings are added as stub rows and delisted symbols are removed.

New symbols are discovered automatically (`discoverSymbols` in
`internal/refresher`) and inserted as "stub" rows, which are always
eligible for refresh due to their epoch `last_updated` timestamp.

## Running it

### 1. Get a free API key
Sign up at https://site.financialmodelingprep.com/ and grab your API key.

### 2. Generate the gRPC code
```
cp .env.example .env   # fill in FMP_API_KEY at minimum
make proto             # requires buf (https://buf.build), or:
make proto-protoc      # requires local protoc + protoc-gen-go + protoc-gen-go-grpc
```

### 3a. Run with Docker Compose (recommended)
```
export FMP_API_KEY=your_key_here
make docker-up
```
This starts Postgres and the service together; the Containerfile generates
the gRPC code and builds the binary in a multi-stage build.

### 3b. Run locally
```
# start a local Postgres, then:
export $(cat .env | xargs)
make run
```

The service listens on `:50051` (configurable via `GRPC_PORT`) and
registers gRPC reflection, so you can explore/call it with `grpcurl`
without needing the `.proto` file locally:

```
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 tsx.v1.CompanyService/ListCompanies
grpcurl -plaintext -d '{"symbol": "SHOP.TO"}' localhost:50051 tsx.v1.CompanyService/GetCompany
```

## gRPC API

```protobuf
service CompanyService {
  rpc ListCompanies(ListCompaniesRequest) returns (ListCompaniesResponse);
  rpc GetCompany(GetCompanyRequest) returns (GetCompanyResponse);
}
```

- `ListCompanies` — paginated (keyset pagination via `page_token`, default
  page size 50, max 500), with an optional exact `sector_filter`.
- `GetCompany` — fetch one company by `symbol` (case-insensitive). Returns
  a `NOT_FOUND` gRPC status if the symbol isn't tracked.

See `proto/tsx/v1/tsx.proto` for full message definitions, including the
`Company` message (symbol, name, exchange, sector, industry, ceo,
description, website, headquarters, employees, market_cap, price,
currency, last_updated).

## Notes / next steps for production use

- This ships a minimal `Migrate()` that runs the schema SQL idempotently
  on startup; for a real production system, use a proper migration tool
  (golang-migrate, atlas, etc.) once the schema evolves.
- Add TLS/auth to the gRPC server before exposing it outside a trusted
  network — it currently runs in plaintext for simplicity.
- Consider adding the standard gRPC health-checking protocol
  (`grpc_health_v1`) for orchestrator liveness/readiness probes.
- `go.mod` lists direct dependencies; run `make tidy` (`go mod tidy`) after
  generating the proto code to resolve exact versions and populate
  `go.sum` for your environment.
