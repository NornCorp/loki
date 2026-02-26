# Loki

A fake service simulator for building realistic microservice architectures. Define services in HCL, and Loki spins up HTTP servers, TCP listeners, Connect-RPC endpoints, and reverse proxies -- complete with auto-generated CRUD APIs, fake data, latency injection, and error simulation.

Built for Instruqt labs to demonstrate service mesh patterns, observability, and chaos engineering without requiring real backend infrastructure.

## Quick Start

```bash
# Run a simple HTTP service
loki server -c examples/http-basic.hcl

# Test it
curl http://localhost:8080/hello
# {"message":"Hello from Loki!"}
```

## Service Types

| Type | Description | Example Use Case |
|------|-------------|------------------|
| `http` | REST APIs with routing, resources, and service chaining | User service, product catalog |
| `tcp` | TCP server with pattern matching | Redis-like cache, custom protocols |
| `connect` | Connect-RPC/gRPC services with auto-generated methods | Typed service APIs |
| `proxy` | Reverse proxy with header transforms and route overrides | API gateway, load balancer |

## Configuration

Loki uses HCL for configuration. A config file defines one or more services that Loki runs concurrently.

### Static Handlers

The simplest configuration: define routes with static responses.

```hcl
service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Loki!" })
    }
  }
}
```

### Auto-Generated REST APIs

Define a `resource` block and Loki generates full CRUD endpoints with fake data:

```hcl
service "api" {
  type   = "http"
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

### Service Chaining (Steps)

Services can call other services and aggregate responses using `step` blocks:

```hcl
service "user-service" {
  type   = "http"
  listen = "127.0.0.1:8081"

  resource "user" {
    rows = 10
    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
  }
}

service "api-gateway" {
  type   = "http"
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
service "api" {
  type   = "http"
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
service "api" {
  type   = "http"
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

### TCP Pattern Matching

Simulate text-based protocols with pattern matching:

```hcl
service "redis-like" {
  type   = "tcp"
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

### Connect-RPC

Define gRPC/Connect-RPC services with auto-generated CRUD methods:

```hcl
service "user-api" {
  type    = "connect"
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
service "api-proxy" {
  type   = "proxy"
  listen = "0.0.0.0:8080"
  target = "http://httpbin.org"

  request_headers  = { "X-Proxy" = "loki" }
  response_headers = { "X-Served-By" = "loki-proxy" }

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

### Heimdall Integration

Register services with [Heimdall](../heimdall/) for mesh-based service discovery and topology visualization:

```hcl
heimdall {
  address = "localhost:7946"
}

service "user-service" {
  type   = "http"
  listen = "127.0.0.1:8081"
  # Automatically registered with Heimdall on startup
}
```

## HCL Expressions

Loki supports HCL expressions throughout the configuration:

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
loki server -c config.hcl      # Start services from a config file
loki validate -c config.hcl    # Validate a config file without starting
```

## Examples

| File | Description |
|------|-------------|
| [http-basic.hcl](examples/http-basic.hcl) | Minimal HTTP service with static handlers |
| [http-resources.hcl](examples/http-resources.hcl) | Auto-generated CRUD with fake data |
| [http-fault-injection.hcl](examples/http-fault-injection.hcl) | Latency and error injection at service and handler level |
| [http-gateway.hcl](examples/http-gateway.hcl) | Service chaining with steps and Heimdall |
| [multi-service-mesh.hcl](examples/multi-service-mesh.hcl) | Full multi-service topology |
| [tcp-patterns.hcl](examples/tcp-patterns.hcl) | TCP pattern matching (Redis-like) |
| [connect-rpc.hcl](examples/connect-rpc.hcl) | Connect-RPC with resources, custom methods, and steps |
| [proxy-reverse.hcl](examples/proxy-reverse.hcl) | Reverse proxy with header transforms |

## Project Structure

```
loki/
├── cmd/loki/           Entry point
├── internal/
│   ├── cli/            CLI commands (server, validate)
│   ├── config/         HCL parsing, types, functions, expression context
│   ├── service/
│   │   ├── http/       HTTP service with routing
│   │   ├── tcp/        TCP with pattern matching
│   │   ├── connect/    Connect-RPC service
│   │   ├── proxy/      Reverse proxy
│   │   ├── registry.go Service lifecycle manager
│   │   ├── timing.go   Latency injection
│   │   └── errors.go   Error injection
│   ├── resource/       In-memory resource store (go-memdb)
│   ├── fake/           Fake data generation (gofakeit)
│   ├── step/           Service chaining execution
│   ├── serf/           Heimdall gossip mesh client
│   └── meta/           Service metadata
├── api/                Protocol Buffers
├── pkg/                Generated Connect-RPC code
├── examples/           Configuration examples
└── docs/               Documentation
```
