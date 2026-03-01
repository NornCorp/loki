# HTTP Gateway Example
# Service chaining with steps and Heimdall mesh discovery.
#
# Usage:
#   polymorph server -c examples/http-gateway.hcl
#
# Test:
#   curl http://localhost:8080/dashboard/user-123
#   curl http://localhost:8080/health

heimdall {
  address = "localhost:7946"
}

# User Service (upstream)
service "http" "user-service" {
  listen = "127.0.0.1:8081"

  resource "user" {
    rows = 10
    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
  }
}

# Order Service (upstream)
service "http" "order-service" {
  listen = "127.0.0.1:8082"

  resource "order" {
    rows = 25
    field "id"         { type = "uuid" }
    field "user_id"    { type = "uuid" }
    field "product"    { type = "name" }
    field "total"      { type = "decimal" }
    field "status" {
      type   = "enum"
      values = ["pending", "shipped", "delivered"]
    }
  }
}

# API Gateway (frontend) - aggregates upstream responses
service "http" "api-gateway" {
  listen = "0.0.0.0:8080"

  handle "dashboard" {
    route = "GET /dashboard/:user_id"

    # Step 1: Fetch user details
    step "user" {
      http {
        url    = "${service.user-service.url}/users/${request.params.user_id}"
        method = "GET"
      }
    }

    # Step 2: Fetch user's orders
    step "orders" {
      http {
        url    = "${service.order-service.url}/orders?user_id=${request.params.user_id}"
        method = "GET"
      }
    }

    # Aggregate the response
    response {
      body = jsonencode({
        user   = step.user.body
        orders = step.orders.body
      })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", service = "api-gateway" })
    }
  }
}
