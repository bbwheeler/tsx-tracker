# tsx-tracker

A Go service that tracks companies listed on the Toronto Stock Exchange
(TSX) in a PostgreSQL database, keeps their data fresh, and exposes it
over gRPC. Every 24 hours it syncs the full TSX symbol list — adding new
listings and removing delisted companies.

## Architecture

```
cmd/server/main.go        wires everything together, starts the gRPC server
internal/config           env-var configuration (DB, refresh cadence, API key)
internal/db               Postgres repository (schema, upsert, query, pagination)
internal/provider         Finnhub API client (TSX symbol list)
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

[Finnhub](https://finnhub.io/) has a free-tier REST API with:
- a stock symbols by exchange endpoint (`GET /stock/symbol?exchange=TO`) —
  returns all listed TSX symbols with name, type, and FIGI identifier

The free tier allows 60 requests per minute with no credit card required.
The service only needs one API call per sync cycle to get the full TSX
symbol list.

Every cycle the full TSX symbol list is synced — new listings are added
and delisted symbols are removed.

## Running it

### 1. Get a free API key
Sign up at https://finnhub.io/ and grab your API key (no credit card required).

### 2. Generate the gRPC code
```
cp .env.example .env   # fill in FINNHUB_API_KEY at minimum
make proto             # requires buf (https://buf.build), or:
make proto-protoc      # requires local protoc + protoc-gen-go + protoc-gen-go-grpc
```

### 3a. Run with Docker Compose (recommended)
```
export FINNHUB_API_KEY=your_key_here
make docker-up
```
This starts Postgres and the service together; the Containerfile generates
the gRPC code and builds the binary in a multi-stage build.

### 3b. Deploy with Podman (Debian Linux)

Install Podman:
```
sudo apt update
sudo apt install -y podman podman-compose
```

Build and run:
```
export FINNHUB_API_KEY=your_key_here
podman-compose up -d --build
```

To stop and remove:
```
podman-compose down -v
```

To view logs:
```
podman-compose logs -f
```

### 3c. Deploy with Podman Quadlet (rootless, Debian)

Quadlet lets you manage Podman containers as systemd user services — the
container starts on boot without root.

**Prerequisites — install Podman and set up lingering:**
```
sudo apt update
sudo apt install -y podman systemd-container
sudo loginctl enable-linger $USER
```

Lingering lets systemd user services (including this container) run
without an active login session.

**External Postgres:** The service connects to a PostgreSQL instance
configured via environment variables. Make sure a Postgres server is
reachable at the host/port you specify in the env file (default
`DB_HOST=192.168.1.31:5432`). The database `tsx_tracker` must exist.

**1. Clone the repo and install:**
```
git clone https://github.com/youruser/tsx-tracker.git ~/tsx-tracker
cd ~/tsx-tracker
make quadlet-install
```

This copies the Quadlet unit file to `~/.config/containers/systemd/`,
installs the env file to `~/.config/tsx-tracker/.env.podman`, and runs
`systemctl --user daemon-reload`.

**2. Edit the environment file** with your credentials:
```
$EDITOR ~/.config/tsx-tracker/.env.podman
```

Fill in `DB_USER`, `DB_PASSWORD`, and `FINNHUB_API_KEY` at minimum. See
`.env.podman` in the repo for all available settings.

**3. Build the container image:**
```
make quadlet-build
```

Or equivalently:
```
podman build -t localhost/tsx-tracker:latest .
```

**4. Start the service:**
```
systemctl --user start tsx-tracker.service
```

The `WantedBy=default.target` in the `.container` file ensures the
service starts automatically on boot.

**5. Check status and logs:**
```
systemctl --user status tsx-tracker
podman logs tsx-tracker
```

**Stopping:**
```
systemctl --user stop tsx-tracker
```

To prevent auto-start on boot, remove the wants symlink:
```
rm ~/.config/systemd/user/default.target.wants/tsx-tracker.service
systemctl --user daemon-reload
```

**Updating:** pull new code, rebuild, and restart:
```
cd ~/tsx-tracker
git pull
make quadlet-build
systemctl --user restart tsx-tracker
```

### 3d. Run locally
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
