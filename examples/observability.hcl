# Observability Example
# Demonstrates logging, tracing, and metrics configuration
# with per-service logging overrides.
#
# Usage:
#   loki server -c examples/observability.hcl
#
# Test:
#   curl http://localhost:8080/hello
#   curl http://localhost:8081/health

logging {
  level  = "debug"
  format = "json"
  output = "stdout"
}

tracing {
  enabled  = true
  endpoint = "localhost:4318"
  sampler  = "ratio"
  ratio    = 0.5
}

metrics {
  enabled = true
  path    = "/metrics"
}

service "http" "api" {
  listen = "0.0.0.0:8080"

  logging {
    level  = "info"
    output = "/tmp/api.log"
  }

  handle "hello" {
    route = "GET /hello"
    response {
      status = 200
      body   = "hello from api"
    }
  }
}

service "http" "worker" {
  listen = "0.0.0.0:8081"

  logging {
    level = "warn"
  }

  handle "health" {
    route = "GET /health"
    response {
      status = 200
      body   = "ok"
    }
  }
}
