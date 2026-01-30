# Error Handling Example
# Tests that step errors are handled gracefully

service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "failing-step" {
    route = "GET /test"

    # This step will fail because the URL is invalid
    step "bad-request" {
      http {
        url    = "http://invalid-host-that-does-not-exist.local:9999/test"
        method = "GET"
      }
    }

    response {
      body = jsonencode({ result = "should not reach here" })
    }
  }
}
