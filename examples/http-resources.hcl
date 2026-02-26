# HTTP Resources Example
# Resource CRUD auto-generation with fake data.
#
# Usage:
#   loki server -c examples/http-resources.hcl
#
# Auto-generated endpoints:
#   GET    /users        - List all users
#   GET    /users/:id    - Get a user by ID
#   POST   /users        - Create a user
#   PUT    /users/:id    - Update a user
#   DELETE /users/:id    - Delete a user
#   GET    /health       - Health check

service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

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

    field "age" {
      type = "int"
      min  = 18
      max  = 65
    }

    field "active" {
      type = "bool"
    }

    field "created_at" {
      type = "datetime"
    }
  }

  # Manual handlers can coexist with auto-generated resource endpoints
  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy" })
    }
  }
}
