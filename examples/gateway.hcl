# Gateway Example - Service Chaining Demo
# This demonstrates calling upstream services and aggregating responses

# User Service (upstream)
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

# Order Service (upstream)
service "order-service" {
  type   = "http"
  listen = "127.0.0.1:8082"

  resource "order" {
    rows = 25
    field "id"         { type = "uuid" }
    field "user_id"    { type = "uuid" }
    field "product"    { type = "name" }
    field "total"      { type = "decimal" }
    field "status"     { type = "enum", values = ["pending", "shipped", "delivered"] }
  }
}

# API Gateway (frontend)
service "api-gateway" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "dashboard" {
    route = "GET /dashboard/:user_id"

    # Step 1: Fetch user details
    step "user" {
      http {
        url    = "http://127.0.0.1:8081/users/${request.query.user_id}"
        method = "GET"
      }
    }

    # Step 2: Fetch user's orders
    step "orders" {
      http {
        url    = "http://127.0.0.1:8082/orders?user_id=${request.query.user_id}"
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
