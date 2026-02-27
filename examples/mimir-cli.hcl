# Example: Generate a fake Mimir (Vault-like) CLI
#
# Build: loki build cli -c examples/mimir-cli.hcl -o mimir
# Usage: ./mimir kv get mysecret

cli "mimir" {
  description = "Interact with Mimir secrets engine"

  flag "address" {
    short       = "a"
    default     = "http://localhost:8200"
    env         = "MIMIR_ADDR"
    description = "Mimir server address"
  }

  flag "token" {
    short       = "t"
    default     = ""
    env         = "MIMIR_TOKEN"
    description = "Authentication token"
  }

  command "kv" {
    description = "Key-value secrets engine"

    command "get" {
      description = "Read a secret"

      arg "path" { required = true }

      action {
        step "read" {
          http {
            url    = "${flag.address}/v1/secret/data/${arg.path}"
            method = "GET"
          }
        }

        output {
          format = "json"
          data   = step.read.body.data
        }
      }
    }

    command "list" {
      description = "List secrets"

      arg "path" { required = true }

      action {
        step "list" {
          http {
            url    = "${flag.address}/v1/secret/metadata/${arg.path}"
            method = "GET"
          }
        }

        output {
          format = "json"
          data   = step.list.body.data.keys
        }
      }
    }
  }

  command "status" {
    description = "Show server status"

    action {
      step "health" {
        http {
          url    = "${flag.address}/v1/sys/health"
          method = "GET"
        }
      }

      output {
        format = "json"
        data   = step.health.body
      }
    }
  }
}
