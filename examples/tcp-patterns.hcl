# TCP Pattern Matching Example
# TCP service with pattern matching, simulating a Redis-like protocol.
#
# Usage:
#   polymorph server -c examples/tcp-patterns.hcl
#
# Test with netcat:
#   nc localhost 6379
#   PING
#   +PONG
#   GET mykey
#   $5
#   hello
#   UNKNOWN
#   -ERR unknown command

service "tcp" "redis-like" {
  listen = "0.0.0.0:6379"

  # Pattern matching with wildcards
  # * matches any sequence of characters

  handle "ping" {
    pattern = "PING*"
    response {
      body = "+PONG\r\n"
    }
  }

  handle "get" {
    pattern = "GET *"
    response {
      body = "$5\r\nhello\r\n"
    }
  }

  handle "set" {
    pattern = "SET * *"
    response {
      body = "+OK\r\n"
    }
  }

  handle "del" {
    pattern = "DEL *"
    response {
      body = ":1\r\n"
    }
  }

  handle "exists" {
    pattern = "EXISTS *"
    response {
      body = ":1\r\n"
    }
  }

  # Default response for unknown commands
  handle "default" {
    response {
      body = "-ERR unknown command\r\n"
    }
  }
}
