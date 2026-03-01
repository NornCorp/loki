package cligen

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/stretchr/testify/require"
)

func parseCLIConfig(t *testing.T, src string) *config.CLIConfig {
	t.Helper()
	file, diags := hclsyntax.ParseConfig([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors(), "parse error: %s", diags.Error())

	var cfg config.Config
	diags = gohcl.DecodeBody(file.Body, &hcl.EvalContext{Functions: config.Functions()}, &cfg)
	require.False(t, diags.HasErrors(), "decode error: %s", diags.Error())
	require.NotNil(t, cfg.CLI, "no cli block found")
	return cfg.CLI
}

func TestGenerateSource_MinimalCLI(t *testing.T) {
	cfg := parseCLIConfig(t, `
cli "hello" {
  description = "A hello CLI"

  command "greet" {
    description = "Say hello"
    arg "name" { required = true }
    action {
      output {
        format = "text"
        data   = arg.name
      }
    }
  }
}
`)

	src, err := GenerateSource(cfg)
	require.NoError(t, err)

	code := string(src)
	require.Contains(t, code, `Use:   "hello"`)
	require.Contains(t, code, `Short: "A hello CLI"`)
	require.Contains(t, code, `Use:   "greet <name>"`)
	require.Contains(t, code, `cobra.ExactArgs(1)`)
	require.Contains(t, code, `outputText`)
}

func TestGenerateSource_WithFlags(t *testing.T) {
	cfg := parseCLIConfig(t, `
cli "mycli" {
  flag "server" {
    short   = "s"
    default = "http://localhost:8080"
    env     = "SERVER_ADDR"
    description = "Server address"
  }

  command "status" {
    action {
      step "check" {
        http {
          url    = "${flag.server}/health"
          method = "GET"
        }
      }
      output {
        format = "json"
        data   = step.check.body
      }
    }
  }
}
`)

	src, err := GenerateSource(cfg)
	require.NoError(t, err)

	code := string(src)
	require.Contains(t, code, "flagServer")
	require.Contains(t, code, `envOr("SERVER_ADDR"`)
	require.Contains(t, code, `httpStep("GET"`)
	require.Contains(t, code, `outputJSON`)
}

func TestGenerateSource_NestedCommands(t *testing.T) {
	cfg := parseCLIConfig(t, `
cli "tool" {
  flag "addr" {
    default = "http://localhost"
  }

  command "kv" {
    command "get" {
      arg "key" { required = true }
      action {
        step "fetch" {
          http {
            url    = "${flag.addr}/kv/${arg.key}"
            method = "GET"
          }
        }
        output {
          format = "json"
          data   = step.fetch.body
        }
      }
    }

    command "list" {
      action {
        step "ls" {
          http {
            url    = "${flag.addr}/kv"
            method = "GET"
          }
        }
        output {
          format = "json"
          data   = step.ls.body
        }
      }
    }
  }
}
`)

	src, err := GenerateSource(cfg)
	require.NoError(t, err)

	code := string(src)
	// Verify nested command structure
	require.Contains(t, code, "cmdKv")
	require.Contains(t, code, "cmdGet")
	require.Contains(t, code, "cmdList")
	require.Contains(t, code, "AddCommand")
}

func TestGenerateSource_TableOutput(t *testing.T) {
	cfg := parseCLIConfig(t, `
cli "mycli" {
  flag "addr" {
    default = "http://localhost"
  }

  command "list" {
    action {
      step "fetch" {
        http {
          url    = "${flag.addr}/items"
          method = "GET"
        }
      }
      output {
        format  = "table"
        data    = step.fetch.body
        columns = ["id", "name", "status"]
      }
    }
  }
}
`)

	src, err := GenerateSource(cfg)
	require.NoError(t, err)

	code := string(src)
	require.Contains(t, code, "outputTable")
	require.Contains(t, code, "tabwriter")
	require.Contains(t, code, `"id"`)
	require.Contains(t, code, `"name"`)
	require.Contains(t, code, `"status"`)
}

func TestGenerateSource_MimirExample(t *testing.T) {
	// Test the actual mimir-cli.hcl example config
	cfg := parseCLIConfig(t, `
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
`)

	src, err := GenerateSource(cfg)
	require.NoError(t, err)

	code := string(src)
	// Verify key structural elements
	require.Contains(t, code, `"mimir"`)
	require.Contains(t, code, "flagAddress")
	require.Contains(t, code, "flagToken")
	require.Contains(t, code, "cmdKv")
	require.Contains(t, code, "cmdGet")
	require.Contains(t, code, "cmdList")
	require.Contains(t, code, "cmdStatus")
	require.Contains(t, code, "stepReadResult")
	require.Contains(t, code, "stepListResult")
	require.Contains(t, code, "stepHealthResult")
	require.Contains(t, code, `envOr("MIMIR_ADDR"`)
	require.Contains(t, code, `envOr("MIMIR_TOKEN"`)

	// Verify the code is valid Go (no format errors)
	require.True(t, strings.HasPrefix(code, "package main"), "generated code should start with package main")
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "Hello"},
		{"hello_world", "HelloWorld"},
		{"hello-world", "HelloWorld"},
		{"kv.get", "KvGet"},
		{"my_long_name", "MyLongName"},
		{"address", "Address"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.expected, toCamelCase(tt.input))
		})
	}
}
