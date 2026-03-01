# OpenAPI Spec Example
# Serves fake responses matching an OpenAPI 3.x spec.
#
# Usage:
#   polymorph server -c examples/openapi-spec.hcl
#
# All paths from the spec are served automatically.
# Override specific endpoints with handle blocks.
#
# Endpoints (from petstore.yaml):
#   GET    /pets          - List all pets (array of 10 items)
#   POST   /pets          - Create a pet
#   GET    /pets/:petId   - Get a pet by ID
#   PUT    /pets/:petId   - Update a pet
#   DELETE /pets/:petId   - Delete a pet

service "http" "pet-store" {
  listen = "0.0.0.0:8080"

  spec {
    path = "./petstore.yaml"
    rows = 10
    seed = 42
  }

  timing {
    p50 = "20ms"
    p90 = "100ms"
    p99 = "300ms"
  }
}
