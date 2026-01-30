# Simple Gateway Example - Service Chaining Test

# User Service (upstream)
service "user-service" {
  type   = "http"
  listen = "127.0.0.1:8081"

  handle "get-user" {
    route = "GET /user"
    response {
      body = jsonencode({
        id   = "user-123"
        name = "Alice"
      })
    }
  }
}

# Order Service (upstream)
service "order-service" {
  type   = "http"
  listen = "127.0.0.1:8082"

  handle "get-orders" {
    route = "GET /orders"
    response {
      body = jsonencode([
        { id = "order-1", product = "Widget", total = 29.99 },
        { id = "order-2", product = "Gadget", total = 49.99 }
      ])
    }
  }
}

# API Gateway (frontend)
service "api-gateway" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "dashboard" {
    route = "GET /dashboard"

    # Step 1: Fetch user details
    step "user" {
      http {
        url    = "http://127.0.0.1:8081/user"
        method = "GET"
      }
    }

    # Step 2: Fetch user's orders
    step "orders" {
      http {
        url    = "http://127.0.0.1:8082/orders"
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
