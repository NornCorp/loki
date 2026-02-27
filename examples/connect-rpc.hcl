# Connect-RPC Example
# Demonstrates resources, custom methods, method overrides, and steps.
#
# Usage:
#   loki server -c examples/connect-rpc.hcl
#
# Test with grpcurl or curl:
#   grpcurl -plaintext localhost:8080 api.v1.UserService/ListUsers
#   grpcurl -plaintext -d '{"id":"abc"}' localhost:8080 api.v1.UserService/GetUser
#   grpcurl -plaintext -d '{}' localhost:8080 api.v1.UserService/SearchUsers
#   grpcurl -plaintext -d '{"user_id":"abc"}' localhost:8080 api.v1.UserService/GetUserWithDetails

service "connect" "user-api" {
  listen  = "0.0.0.0:8080"
  package = "api.v1"

  # Auto-generated CRUD: ListUsers, GetUser, CreateUser, UpdateUser, DeleteUser
  resource "user" {
    rows = 20

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
      max  = 80
    }
  }

  # Custom method: SearchUsers - returns filtered data
  handle "SearchUsers" {
    response {
      body = jsonencode({
        users = [
          {
            id    = uuid()
            name  = "Custom User 1"
            email = "custom1@example.com"
            age   = 25
          },
          {
            id    = uuid()
            name  = "Custom User 2"
            email = "custom2@example.com"
            age   = 30
          }
        ]
        query = request.query
      })
    }
  }

  # Custom method: GetUserStats - uses request parameters
  handle "GetUserStats" {
    response {
      body = jsonencode({
        total_users  = 20
        requested_id = request.id
        timestamp    = timestamp()
      })
    }
  }

  # Custom method with steps: GetUserWithDetails - calls upstream service
  handle "GetUserWithDetails" {
    step "user" {
      http {
        url    = "${service.user-api.url}/api.v1.UserService/GetUser"
        method = "POST"
        body   = jsonencode({ id = request.user_id })
      }
    }

    response {
      body = jsonencode({
        user = step.user.body
        metadata = {
          fetched_at = timestamp()
          source     = "custom_method"
        }
      })
    }
  }

  # Override auto-generated GetUser with custom response
  handle "GetUser" {
    response {
      body = jsonencode({
        id   = "override-id"
        name = "Overridden User"
        note = "This is a custom override of the auto-generated GetUser method"
      })
    }
  }

  # Echo method - echoes back request parameters
  handle "Echo" {
    response {
      body = jsonencode({
        message  = "Echo response"
        received = request
      })
    }
  }
}
