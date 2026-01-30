service "api" {
  type   = "http"
  listen = "0.0.0.0:8080"

  handle "test" {
    route = "GET /test"
    response {
      body = jsonencode({
        id        = uuid()
        timestamp = timestamp()
        data      = { foo = "bar", num = 42 }
      })
    }
  }
}
