service "http" "backend" {
  listen = "127.0.0.1:8081"

  handle "hello" {
    route = "GET /hello"
    response {
      body = jsonencode({ message = "hello" })
    }
  }
}

service "proxy" "proxy" {
  listen = "0.0.0.0:8080"
  target = service.backend.url
}
