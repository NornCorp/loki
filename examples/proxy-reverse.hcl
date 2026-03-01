# Reverse Proxy Example
# Reverse proxy with request/response header transforms and route overrides.
# Also demonstrates internal proxying with service.* references.
#
# Usage:
#   polymorph server -c examples/proxy-reverse.hcl
#
# Test:
#   curl http://localhost:8080/get        # Proxied to httpbin with custom headers
#   curl http://localhost:8080/health     # Handled locally (route override)
#   curl http://localhost:8080/status     # Handled locally (route override)
#   curl http://localhost:8090/hello      # Proxied to internal backend via service ref

# External reverse proxy
service "proxy" "api-proxy" {
  listen   = "0.0.0.0:8080"
  target   = "http://httpbin.org"

  # Add headers to all proxied requests
  request_headers = {
    "X-Proxy"      = "polymorph"
    "X-Custom-Tag" = "demo"
  }

  # Add headers to all proxied responses
  response_headers = {
    "X-Served-By" = "polymorph-proxy"
  }

  # Override specific routes (handled locally, not proxied)
  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({
        proxy  = "healthy"
        status = "ok"
      })
    }
  }

  handle "status" {
    route = "GET /status"
    response {
      status = 200
      body = jsonencode({
        upstream = "http://httpbin.org"
        service  = "api-proxy"
      })
    }
  }
}

# Internal backend service
service "http" "backend" {
  listen = "127.0.0.1:8081"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from backend" })
    }
  }
}

# Internal proxy using service.* reference
service "proxy" "internal-proxy" {
  listen = "0.0.0.0:8090"
  target = service.backend.url
}
