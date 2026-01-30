# Mesh Demo - Complex Service Topology with Heimdall
# This demonstrates a realistic microservices architecture with service discovery

# Register all services with Heimdall
heimdall {
  address = "localhost:7946"
}

# Product Catalog Service
service "product-service" {
  type   = "http"
  listen = "127.0.0.1:8083"

  resource "product" {
    rows = 50

    field "id" {
      type = "uuid"
    }

    field "name" {
      type = "product_name"
    }

    field "price" {
      type = "decimal"
    }

    field "in_stock" {
      type = "bool"
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", service = "product-service" })
    }
  }
}

# User Service
service "user-service" {
  type   = "http"
  listen = "127.0.0.1:8081"

  timing {
    p50 = "10ms"
    p90 = "50ms"
    p99 = "200ms"
  }

  resource "user" {
    rows = 100

    field "id" {
      type = "uuid"
    }

    field "name" {
      type = "name"
    }

    field "email" {
      type = "email"
    }

    field "created_at" {
      type = "datetime"
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", service = "user-service" })
    }
  }
}

# Order Service
service "order-service" {
  type   = "http"
  listen = "127.0.0.1:8082"

  timing {
    p50 = "15ms"
    p90 = "75ms"
    p99 = "250ms"
  }

  resource "order" {
    rows = 200

    field "id" {
      type = "uuid"
    }

    field "user_id" {
      type = "uuid"
    }

    field "product_id" {
      type = "uuid"
    }

    field "total" {
      type = "decimal"
    }

    field "created_at" {
      type = "datetime"
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", service = "order-service" })
    }
  }
}

# Analytics Service
service "analytics-service" {
  type      = "http"
  listen    = "127.0.0.1:8084"
  upstreams = ["user-service", "order-service"]

  timing {
    p50 = "50ms"
    p90 = "200ms"
    p99 = "500ms"
  }

  handle "metrics" {
    route = "GET /metrics"
    response {
      body = jsonencode({
        total_users = 1000
        total_orders = 5000
        revenue = 125000.50
      })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy", service = "analytics-service" })
    }
  }
}

# API Gateway - Frontend service
service "api-gateway" {
  type      = "http"
  listen    = "0.0.0.0:8080"
  upstreams = ["user-service", "order-service", "product-service", "analytics-service"]

  timing {
    p50 = "20ms"
    p90 = "100ms"
    p99 = "300ms"
  }

  error "rate_limit" {
    rate   = 0.001
    status = 429
    response {
      body = jsonencode({ error = "rate_limited" })
    }
  }

  handle "status" {
    route = "GET /status"
    response {
      body = jsonencode({
        service = "api-gateway",
        version = "1.0.0",
        upstreams = {
          user_service = "127.0.0.1:8081"
          order_service = "127.0.0.1:8082"
          product_service = "127.0.0.1:8083"
          analytics_service = "127.0.0.1:8084"
        }
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
