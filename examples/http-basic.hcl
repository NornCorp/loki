# HTTP Basic Example
# Minimal HTTP service with static request handlers.
#
# Usage:
#   loki server -c examples/http-basic.hcl
#
# Test:
#   curl http://localhost:8080/hello
#   curl http://localhost:8080/health

service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Loki!" })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy" })
    }
  }
}
