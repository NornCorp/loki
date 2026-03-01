# Polymorph

<img src="polymorph.png" alt="Polymorph" width="400">

A fake service simulator for building realistic microservice architectures. Define services in HCL, and Polymorph spins up HTTP servers, TCP listeners, Connect-RPC endpoints, and reverse proxies -- complete with auto-generated CRUD APIs, fake data, latency injection, and error simulation.

Built for Instruqt labs to demonstrate service mesh patterns, observability, and chaos engineering without requiring real backend infrastructure.

## Quick Start

```bash
# Run a simple HTTP service
polymorph server -c examples/http-basic.hcl

# Test it
curl http://localhost:8080/hello
# {"message":"Hello from Polymorph!"}
```

## Service Types

| Type | Description | Example Use Case |
|------|-------------|------------------|
| `http` | REST APIs with routing, resources, and service chaining | User service, product catalog |
| `tcp` | TCP server with pattern matching | Redis-like cache, custom protocols |
| `connect` | Connect-RPC/gRPC services with auto-generated methods | Typed service APIs |
| `proxy` | Reverse proxy with header transforms and route overrides | API gateway, load balancer |
| `postgres` | PostgreSQL wire protocol with SQL query handling | Database, data store |

## Configuration

Polymorph uses HCL for configuration. A config file defines one or more services that Polymorph runs concurrently. You can also pass a directory to `-c`, in which case all `*.hcl` files in it are loaded and merged (non-recursive, sorted by filename). This lets you split large configs into separate files (e.g. `01-logging.hcl`, `02-services.hcl`) while cross-file `service.*` references work as expected.

### Static Handlers

The simplest configuration: define routes with static responses.

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Polymorph!" })
    }
  }
}
```

### Auto-Generated REST APIs

Define a `resource` block and Polymorph generates full CRUD endpoints with fake data:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  resource "user" {
    rows = 100

    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
    field "age"   { type = "int", min = 18, max = 65 }
    field "active" { type = "bool" }
    field "created_at" { type = "datetime" }
  }
}
```

This generates:

```
GET    /users        List all users
GET    /users/:id    Get a user by ID
POST   /users        Create a user
PUT    /users/:id    Update a user
DELETE /users/:id    Delete a user
```

The resource name is automatically pluralized for endpoint paths. See [docs/fake-data-types.md](docs/fake-data-types.md) for the full list of 70+ supported data types.

### OpenAPI Spec

Serve fake responses from an OpenAPI 3.x spec. Polymorph parses the spec at startup, generates mock JSON for each operation's response schema, and serves them on the matching routes.

```hcl
service "http" "pet-store" {
  listen = "0.0.0.0:8080"

  spec {
    path = "./petstore.yaml"
    rows = 10
    seed = 42
  }

  timing {
    p50 = "20ms"
    p90 = "100ms"
    p99 = "300ms"
  }
}
```

This reads the OpenAPI spec and auto-generates endpoints for every path + operation. Array responses contain `rows` items (default 10). Set `seed` for deterministic output across restarts.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | (required) | Path to an OpenAPI 3.0 or 3.1 YAML/JSON spec file |
| `rows` | int | 10 | Number of items in array responses |
| `seed` | int | (random) | Random seed for deterministic mock data |

Manual `handle` and `resource` blocks take priority over spec routes -- use them to override specific endpoints while the spec handles everything else.

```hcl
service "http" "pet-store" {
  listen = "0.0.0.0:8080"

  spec {
    path = "./petstore.yaml"
  }

  # Override the list endpoint with custom behavior
  handle "custom-list" {
    route = "GET /pets"
    response {
      body = jsonencode({ custom = true })
    }
  }
}
```

Service-level `timing`, `error`, `rate_limit`, and `cors` blocks apply to spec routes the same way they apply to regular handlers. See [examples/openapi-spec.hcl](examples/openapi-spec.hcl).

### Service Chaining (Steps)

Services can call other services and aggregate responses using `step` blocks:

```hcl
service "http" "user-service" {
  listen = "127.0.0.1:8081"

  resource "user" {
    rows = 10
    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
  }
}

service "http" "api-gateway" {
  listen = "0.0.0.0:8080"

  handle "dashboard" {
    route = "GET /dashboard/:user_id"

    step "user" {
      http {
        url    = "${service.user-service.url}/users/${request.params.user_id}"
        method = "GET"
      }
    }

    response {
      body = jsonencode({
        user = step.user.body
      })
    }
  }
}
```

### Latency Injection

Add realistic percentile-based latency at the service or handler level:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  # Service-level: applies to all handlers
  timing {
    p50      = "10ms"
    p90      = "50ms"
    p99      = "200ms"
    variance = 0.1
  }

  # Handler-level: overrides service-level for this route
  handle "slow" {
    route = "GET /slow"
    timing {
      p50 = "200ms"
      p90 = "500ms"
      p99 = "1s"
    }
    response {
      body = jsonencode({ endpoint = "slow" })
    }
  }
}
```

### Error Injection

Simulate failures at a configured rate:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  error "server_error" {
    rate   = 0.01   # 1% of requests
    status = 503
    response {
      body = jsonencode({ error = "service_unavailable" })
    }
  }

  error "rate_limit" {
    rate   = 0.005  # 0.5% of requests
    status = 429
    response {
      headers = { "Retry-After" = "60" }
      body = jsonencode({ error = "rate_limited" })
    }
  }
}
```

Error blocks can also be defined at the handler level to override service defaults.

### CORS

Enable cross-origin requests for browser-based clients:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  cors {
    allowed_origins = ["*"]
  }
}
```

Or with specific origins and credentials:

```hcl
cors {
  allowed_origins   = ["https://example.com", "https://app.example.com"]
  allowed_methods   = ["GET", "POST", "PUT", "DELETE"]
  allowed_headers   = ["Content-Type", "Authorization"]
  allow_credentials = true
}
```

When no `cors` block is present, no CORS headers are sent. `allowed_methods` and `allowed_headers` have sensible defaults if omitted.

### Load Generation

Simulate CPU and memory load during request handling for autoscaling and resource limit demos:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  load {
    cpu_cores   = 2      # goroutines doing busy work
    cpu_percent = 50     # duty cycle per core
    memory      = "256MB" # allocate and hold per request
  }

  handle "work" {
    route = "GET /work"
    response {
      body = jsonencode({ status = "done" })
    }
  }
}
```

CPU load uses a duty cycle (busy loop + sleep in 10ms windows). Memory is allocated and touched per request, then released when the response completes. Useful for Kubernetes HPA, resource limits, and OOMKill demos.

### Rate Limiting

Throttle requests beyond a configured rate:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8080"

  # Service-level: applies to all handlers
  rate_limit {
    rps    = 100
    status = 429
    response {
      headers = { "Retry-After" = "1" }
      body = jsonencode({ error = "rate_limited" })
    }
  }

  # Handler-level: overrides service-level for this route
  handle "search" {
    route = "GET /search"

    rate_limit {
      rps    = 10
      status = 429
    }

    response {
      body = jsonencode({ results = [] })
    }
  }
}
```

Uses a token bucket algorithm -- requests are allowed up to the RPS rate with burst capacity equal to the RPS value. Unlike error injection (probabilistic), rate limiting is deterministic based on actual request volume.

### Static Files

Serve files from a directory:

```hcl
service "http" "cdn" {
  listen = "0.0.0.0:8080"

  static {
    root = "./public"
  }
}
```

Or mounted at a URL prefix:

```hcl
static {
  route = "/assets"
  root  = "./public"
}
```

Explicit handlers and resources take priority over static files.

### TCP Pattern Matching

Simulate text-based protocols with pattern matching:

```hcl
service "tcp" "redis-like" {
  listen = "0.0.0.0:6379"

  handle "ping" {
    pattern = "PING*"
    response { body = "+PONG\r\n" }
  }

  handle "get" {
    pattern = "GET *"
    response { body = "$5\r\nhello\r\n" }
  }

  # Default for unmatched input
  handle "default" {
    response { body = "-ERR unknown command\r\n" }
  }
}
```

### PostgreSQL

Simulate a PostgreSQL database with tables, fake data, and SQL query handling. Clients like `psql` and `pgcli` can connect and run queries against auto-generated data.

```hcl
service "postgres" "db" {
  listen = "0.0.0.0:5432"

  auth {
    users    = { "app" = "secret" }
    database = "myapp"
  }

  table "user" {
    rows = 100
    column "id"    { type = "uuid" }
    column "name"  { type = "name" }
    column "email" { type = "email" }
  }

  table "order" {
    rows = 50
    column "id"      { type = "uuid" }
    column "user_id" { type = "uuid" }

    column "total" {
      type = "decimal"
      min  = 10
      max  = 500
    }

    column "status" {
      type   = "enum"
      values = ["pending", "shipped", "delivered"]
    }

    column "created_at" { type = "datetime" }
  }

  # Custom query override
  query "select * from users where status = *" {
    from_table = "user"
    where      = "status = ${2}"
  }
}
```

Supports:

- MD5 password authentication (or trust mode when no `auth` block)
- `SELECT`, `INSERT`, `UPDATE`, `DELETE` with auto-generated fake data
- WHERE clause filtering and LIMIT
- Table name pluralization (`table "user"` responds to `SELECT * FROM users`)
- Custom query patterns with wildcard captures (`*` → `${1}`, `${2}`, ...)
- System catalog queries for client compatibility (`pg_catalog`, `information_schema`)

Connect with any PostgreSQL client:

```bash
psql -h localhost -p 5432 -U app -d myapp
pgcli -h localhost -p 5432 -u app -d myapp
```

### Connect-RPC

Define gRPC/Connect-RPC services with auto-generated CRUD methods:

```hcl
service "connect" "user-api" {
  listen  = "0.0.0.0:8080"
  package = "api.v1"

  # Auto-generates: ListUsers, GetUser, CreateUser, UpdateUser, DeleteUser
  resource "user" {
    rows = 20
    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
  }

  # Custom methods
  handle "SearchUsers" {
    response {
      body = jsonencode({ users = [], query = request.query })
    }
  }
}
```

### Reverse Proxy

Proxy requests to an upstream target with header injection and local route overrides:

```hcl
service "proxy" "api-proxy" {
  listen = "0.0.0.0:8080"
  target = "http://httpbin.org"

  request_headers  = { "X-Proxy" = "polymorph" }
  response_headers = { "X-Served-By" = "polymorph-proxy" }

  # Override specific routes locally
  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "ok" })
    }
  }
}
```

Proxy targets can reference other services: `target = service.backend.url`

### TLS

Enable HTTPS with auto-generated self-signed certificates or your own:

```hcl
service "http" "api" {
  listen = "0.0.0.0:8443"

  tls {}  # auto-generates a self-signed certificate

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello over TLS!" })
    }
  }
}
```

Or with provided certificates:

```hcl
tls {
  cert = "/path/to/cert.pem"
  key  = "/path/to/key.pem"
}
```

TLS works on all service types: `http`, `connect`, `proxy`, `tcp`, and `postgres`. For PostgreSQL, TLS is negotiated via the standard SSL handshake -- clients that request SSL will be upgraded transparently.

### Observability

Configure logging, tracing, and metrics via top-level HCL blocks. All are optional -- defaults match the previous behavior.

```hcl
logging {
  level  = "info"      # debug | info | warn | error (default: "info")
  format = "text"      # text | json (default: "text")
  output = "stderr"    # stdout | stderr | file path (default: "stderr")
}

tracing {
  enabled  = true              # default: true
  endpoint = "localhost:4318"  # OTLP HTTP endpoint; OTEL_EXPORTER_OTLP_ENDPOINT env var works as fallback
  sampler  = "always_on"       # always_on | always_off | parent_based | ratio (default: "always_on")
  ratio    = 0.5               # only used when sampler = "ratio"
}

metrics {
  enabled = true        # default: true
  path    = "/metrics"  # Prometheus scrape path (default: "/metrics")
}
```

Per-service logging overrides are supported inside `service` blocks:

```hcl
service "http" "noisy-api" {
  listen = "0.0.0.0:8080"

  logging {
    level  = "warn"
    output = "/var/log/noisy-api.log"
  }
}
```

Prometheus metrics exposed on HTTP services:

```
polymorph_requests_total{service, handler, status}
polymorph_request_duration_seconds{service, handler}
polymorph_step_duration_seconds{service, handler, step}
polymorph_errors_total{service, handler, type}
```

Traces include spans for request handling and step execution, with context propagated through the step chain. See [examples/observability.hcl](examples/observability.hcl) for a full demo.

### Heimdall Integration

Register services with [Heimdall](../heimdall/) for mesh-based service discovery and topology visualization:

```hcl
heimdall {
  address = "localhost:7946"
}

service "http" "user-service" {
  listen = "127.0.0.1:8081"
  # Automatically registered with Heimdall on startup
}
```

## HCL Expressions

Polymorph supports HCL expressions throughout the configuration:

| Expression | Description |
|------------|-------------|
| `jsonencode({...})` | Encode a value as JSON |
| `uuid()` | Generate a UUID |
| `timestamp()` | Current ISO 8601 timestamp |
| `service.<name>.url` | URL of another service in the config |
| `service.<name>.address` | Listen address of another service |
| `request.params.<name>` | URL path parameter |
| `request.query.<name>` | Query string parameter |
| `request.body` | Request body |
| `step.<name>.body` | Response body from a step |
| `step.<name>.status` | HTTP status from a step |

## CLI

```bash
polymorph server -c config.hcl                          # Start services from a config file
polymorph server -c config.d/                           # Load all *.hcl files from a directory
polymorph validate -c config.hcl                        # Validate a config file without starting
polymorph cli -c cli-config.hcl -- <args>               # Run a CLI defined in an HCL config
```

### CLI Runtime

Run CLIs defined in HCL directly -- no code generation or Go toolchain required. Polymorph builds the command tree at runtime and executes steps using the built-in step executor.

```hcl
cli "mimir" {
  description = "Interact with Mimir secrets engine"

  flag "address" {
    short   = "a"
    default = "http://localhost:8200"
    env     = "MIMIR_ADDR"
  }

  command "kv" {
    description = "Key-value secrets engine"

    command "get" {
      description = "Read a secret"
      arg "path" { required = true }

      action {
        step "read" {
          http {
            url    = "${flag.address}/v1/secret/data/${arg.path}"
            method = "GET"
          }
        }
        output {
          format = "json"
          data   = step.read.body.data
        }
      }
    }
  }

  command "status" {
    description = "Show server status"
    action {
      step "health" {
        http {
          url    = "${flag.address}/v1/sys/health"
          method = "GET"
        }
      }
      output {
        format = "json"
        data   = step.health.body
      }
    }
  }
}
```

```bash
# Run directly
polymorph cli -c examples/mimir-cli.hcl -- --help
polymorph cli -c examples/mimir-cli.hcl -- kv get mysecret
polymorph cli -c examples/mimir-cli.hcl -- -a http://vault:8200 status

# Or alias for a seamless experience
alias mimir='polymorph cli -c /path/to/mimir-cli.hcl --'
mimir kv get mysecret
mimir -a http://vault:8200 status
```

Expressions in CLI configs support `flag.*` for flags, `arg.*` for positional arguments, and `step.*` for accessing step results. Flags support env var defaults via the `env` field. Output formats: `json`, `table`, `text`.

## Examples

| File | Description |
|------|-------------|
| [http-basic.hcl](examples/http-basic.hcl) | Minimal HTTP service with static handlers |
| [http-resources.hcl](examples/http-resources.hcl) | Auto-generated CRUD with fake data |
| [openapi-spec.hcl](examples/openapi-spec.hcl) | Serve fake responses from an OpenAPI spec |
| [http-fault-injection.hcl](examples/http-fault-injection.hcl) | Latency and error injection at service and handler level |
| [http-gateway.hcl](examples/http-gateway.hcl) | Service chaining with steps and Heimdall |
| [multi-service-mesh.hcl](examples/multi-service-mesh.hcl) | Full multi-service topology |
| [tcp-patterns.hcl](examples/tcp-patterns.hcl) | TCP pattern matching (Redis-like) |
| [postgres.hcl](examples/postgres.hcl) | PostgreSQL with tables, auth, and custom queries |
| [connect-rpc.hcl](examples/connect-rpc.hcl) | Connect-RPC with resources, custom methods, and steps |
| [proxy-reverse.hcl](examples/proxy-reverse.hcl) | Reverse proxy with header transforms |
| [https.hcl](examples/https.hcl) | HTTPS with auto-generated self-signed certificate |
| [observability.hcl](examples/observability.hcl) | Logging, tracing, and metrics configuration |
| [mimir-cli.hcl](examples/mimir-cli.hcl) | CLI runtime: fake Vault-like CLI tool |

## Project Structure

```
polymorph/
├── cmd/polymorph/      Entry point
├── internal/
│   ├── cli/            CLI commands (server, validate, cli)
│   ├── config/         HCL parsing, types, functions, expression context
│   ├── service/
│   │   ├── http/       HTTP service with routing
│   │   ├── tcp/        TCP with pattern matching
│   │   ├── postgres/   PostgreSQL wire protocol
│   │   ├── connect/    Connect-RPC service
│   │   ├── proxy/      Reverse proxy
│   │   ├── registry.go Service lifecycle manager
│   │   ├── timing.go   Latency injection
│   │   ├── errors.go   Error injection
│   │   └── tls.go      TLS config and listener wrapping
│   ├── resource/       In-memory resource store (go-memdb)
│   ├── fake/           Fake data generation (gofakeit)
│   ├── step/           Service chaining execution
│   ├── cligen/         CLI runtime interpreter
│   ├── logging/        Structured logging (slog) setup
│   ├── metrics/        Prometheus metrics
│   ├── tracing/        OpenTelemetry tracing
│   ├── serf/           Heimdall gossip mesh client
│   └── meta/           Service metadata
├── api/                Protocol Buffers
├── pkg/                Generated Connect-RPC code
├── examples/           Configuration examples
└── docs/               Documentation
```
