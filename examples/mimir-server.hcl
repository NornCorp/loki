# Fake Mimir (Vault-like) secrets server
#
# Provides the backend for mimir-cli.hcl:
#   polymorph server -c examples/mimir-server.hcl
#   polymorph cli -c examples/mimir-cli.hcl -- kv get mysecret
#   polymorph cli -c examples/mimir-cli.hcl -- kv list secrets
#   polymorph cli -c examples/mimir-cli.hcl -- status

service "http" "mimir" {
  listen = "0.0.0.0:8200"

  handle "health" {
    route = "GET /v1/sys/health"
    response {
      body = jsonencode({
        initialized   = true
        sealed        = false
        standby       = false
        server_time   = timestamp()
        cluster_name  = "mimir-cluster-1"
        version       = "1.15.0"
      })
    }
  }

  handle "kv-get" {
    route = "GET /v1/secret/data/:path"
    response {
      body = jsonencode({
        request_id = uuid()
        path       = request.params.path
        data = {
          data = {
            username = "admin"
            password = "s3cr3t-v4lue"
            api_key  = "ak-29f8a3b1c7e04d5f"
          }
          metadata = {
            created_time  = "2026-01-15T10:30:00Z"
            version       = 3
            destroyed     = false
          }
        }
      })
    }
  }

  handle "kv-list" {
    route = "GET /v1/secret/metadata/:path"
    response {
      body = jsonencode({
        request_id = uuid()
        data = {
          keys = ["database", "api-key", "tls-cert", "oauth-client"]
        }
      })
    }
  }
}
