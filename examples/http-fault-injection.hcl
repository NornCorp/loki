# HTTP Fault Injection Example
# Demonstrates latency and error injection at both service and handler level.
#
# Usage:
#   polymorph server -c examples/http-fault-injection.hcl
#
# Test endpoints:
#   curl http://localhost:8080/api/status      # Service-level timing + errors
#   curl http://localhost:8080/api/users/123   # Service-level timing + errors
#   curl http://localhost:8080/api/fast        # Handler-level timing override (p50=1ms)
#   curl http://localhost:8080/api/slow        # Handler-level timing override (p50=200ms)
#   curl http://localhost:8080/api/unreliable  # Handler-level error override (50% failure)
#   curl http://localhost:8080/api/custom      # Both timing + error overrides

service "http" "api" {
  listen = "0.0.0.0:8080"

  # Service-level latency injection
  timing {
    p50      = "10ms"
    p90      = "50ms"
    p99      = "200ms"
    variance = 0.1
  }

  # Service-level error injection: 1% of requests get 503
  error "server_error" {
    rate   = 0.01
    status = 503
    response {
      body = jsonencode({
        error   = "service_unavailable"
        message = "The service is temporarily unavailable"
        code    = "SVC_UNAVAIL"
      })
    }
  }

  # Service-level error injection: 0.5% of requests get rate limited
  error "rate_limit" {
    rate   = 0.005
    status = 429
    response {
      headers = {
        "Retry-After" = "60"
      }
      body = jsonencode({
        error   = "rate_limited"
        message = "Too many requests, please retry after 60 seconds"
        code    = "RATE_LIMIT"
      })
    }
  }

  # Uses service-level defaults
  handle "status" {
    route = "GET /api/status"
    response {
      body = jsonencode({
        status  = "healthy"
        uptime  = "2h15m"
        version = "1.0.0"
      })
    }
  }

  handle "user" {
    route = "GET /api/users/:id"
    response {
      body = jsonencode({
        id    = "user-123"
        name  = "John Doe"
        email = "john@example.com"
      })
    }
  }

  # Handler-level timing override: very fast
  handle "fast" {
    route = "GET /api/fast"

    timing {
      p50      = "1ms"
      p90      = "5ms"
      p99      = "10ms"
      variance = 0.1
    }

    response {
      body = jsonencode({
        endpoint = "fast"
        message  = "Using handler-level timing (p50=1ms)"
      })
    }
  }

  # Handler-level timing override: very slow
  handle "slow" {
    route = "GET /api/slow"

    timing {
      p50      = "200ms"
      p90      = "500ms"
      p99      = "1s"
      variance = 0.1
    }

    response {
      body = jsonencode({
        endpoint = "slow"
        message  = "Using handler-level timing (p50=200ms)"
      })
    }
  }

  # Handler-level error override: very unreliable
  handle "unreliable" {
    route = "GET /api/unreliable"

    error "frequent_error" {
      rate   = 0.5
      status = 503
      response {
        body = jsonencode({
          error   = "service_unavailable"
          message = "This endpoint is intentionally unreliable (50% failure rate)"
        })
      }
    }

    response {
      body = jsonencode({
        endpoint = "unreliable"
        message  = "You got lucky! This endpoint has a 50% failure rate."
      })
    }
  }

  # Handler-level override of both timing AND errors
  handle "custom" {
    route = "GET /api/custom"

    timing {
      p50      = "25ms"
      p90      = "75ms"
      p99      = "150ms"
      variance = 0.2
    }

    error "custom_timeout" {
      rate   = 0.1
      status = 504
      response {
        headers = { "X-Error-Type" = "Gateway Timeout" }
        body = jsonencode({
          error   = "gateway_timeout"
          message = "Custom error configuration"
        })
      }
    }

    response {
      body = jsonencode({
        endpoint = "custom"
        message  = "Using custom timing (p50=25ms) and errors (10% timeout rate)"
      })
    }
  }
}
