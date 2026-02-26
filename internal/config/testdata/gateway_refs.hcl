service "user-service" {
  type   = "http"
  listen = "127.0.0.1:8081"

  handle "get-user" {
    route = "GET /users"
    response {
      body = jsonencode({ id = "user-1", name = "Alice" })
    }
  }
}

service "order-service" {
  type   = "http"
  listen = "127.0.0.1:8082"

  handle "get-orders" {
    route = "GET /orders"
    response {
      body = jsonencode({ orders = [] })
    }
  }
}

service "api-gateway" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "dashboard" {
    route = "GET /dashboard"

    step "user" {
      http {
        url    = "${service.user-service.url}/users"
        method = "GET"
      }
    }

    step "orders" {
      http {
        url    = "${service.order-service.url}/orders"
        method = "GET"
      }
    }

    response {
      body = jsonencode({
        user   = step.user.body
        orders = step.orders.body
      })
    }
  }
}
