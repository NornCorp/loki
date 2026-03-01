service "http" "api" {
  listen = "0.0.0.0:8080"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "Hello from Polymorph!" })
    }
  }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "healthy" })
    }
  }
}
