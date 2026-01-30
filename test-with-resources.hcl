heimdall {
  address = "localhost:7946"
}

service "users-api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  resource "user" {
    rows = 50

    field "id" {
      type = "uuid"
    }

    field "name" {
      type = "name"
    }

    field "email" {
      type = "email"
    }

    field "age" {
      type = "int"
      min  = 18
      max  = 65
    }

    field "status" {
      type   = "enum"
      values = ["active", "inactive", "pending"]
    }
  }

  resource "order" {
    rows = 100

    field "id" {
      type = "uuid"
    }

    field "user_id" {
      type = "uuid"
    }

    field "amount" {
      type = "decimal"
      min  = 10.0
      max  = 5000.0
    }

    field "status" {
      type   = "enum"
      values = ["pending", "shipped", "delivered", "cancelled"]
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy" })
    }
  }
}
