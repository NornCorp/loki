# HTTPS service with auto-generated self-signed certificate
service "http" "secure-api" {
  listen = "0.0.0.0:8443"

  tls {}  # auto-generates a self-signed certificate

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Polymorph over TLS!" })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", tls = true })
    }
  }
}

# To use with provided certificates:
#
# service "http" "api" {
#   listen = "0.0.0.0:8443"
#
#   tls {
#     cert = "/path/to/cert.pem"
#     key  = "/path/to/key.pem"
#   }
# }
