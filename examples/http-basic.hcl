# HTTP Basic Example
# Minimal HTTP service with static request handlers.
#
# Usage:
#   polymorph server -c examples/http-basic.hcl
#
# Test:
#   curl http://localhost:8080/hello
#   curl http://localhost:8080/health

service "http" "api" {
  listen = "0.0.0.0:8080"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Polymorph!" })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy" })
    }
  }
}
