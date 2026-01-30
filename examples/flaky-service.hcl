// Flaky Service Example
//
// This example demonstrates latency and error injection capabilities.
//
// Usage:
//   loki server -c examples/flaky-service.hcl
//
// Then test with:
//   curl http://localhost:8080/api/status
//   curl http://localhost:8080/api/users/123
//
// You'll notice:
// - Responses have varying latency following the percentile distribution
// - Occasionally you'll get 503 or 429 errors at the configured rates

service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  // Latency injection
  // - 50% of requests will be <= 10ms
  // - 90% of requests will be <= 50ms
  // - 99% of requests will be <= 200ms
  // - Variance adds ±10% randomness to each request
  timing {
    p50      = "10ms"
    p90      = "50ms"
    p99      = "200ms"
    variance = 0.1
  }

  // Error injection: 1% of requests will get a 503 Service Unavailable
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

  // Error injection: 0.5% of requests will get rate limited
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

  // Regular handlers
  handle "status" {
    route = "GET /api/status"
    response {
      body = jsonencode({
        status = "healthy"
        uptime = "2h15m"
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

  handle "slow-operation" {
    route = "POST /api/operations"
    response {
      status = 202
      body = jsonencode({
        message = "Operation accepted"
        id = uuid()
      })
    }
  }
}
