package cligen

import (
	"math/big"
	"testing"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestCtyToInterface_String(t *testing.T) {
	result := CtyToInterface(cty.StringVal("hello"))
	require.Equal(t, "hello", result)
}

func TestCtyToInterface_NumberInt(t *testing.T) {
	result := CtyToInterface(cty.NumberIntVal(42))
	require.Equal(t, int64(42), result)
}

func TestCtyToInterface_NumberFloat(t *testing.T) {
	result := CtyToInterface(cty.NumberVal(new(big.Float).SetFloat64(3.14)))
	require.InDelta(t, 3.14, result, 0.001)
}

func TestCtyToInterface_Bool(t *testing.T) {
	require.Equal(t, true, CtyToInterface(cty.True))
	require.Equal(t, false, CtyToInterface(cty.False))
}

func TestCtyToInterface_Null(t *testing.T) {
	result := CtyToInterface(cty.NullVal(cty.String))
	require.Nil(t, result)
}

func TestCtyToInterface_Object(t *testing.T) {
	val := cty.ObjectVal(map[string]cty.Value{
		"name": cty.StringVal("alice"),
		"age":  cty.NumberIntVal(30),
	})
	result := CtyToInterface(val)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "alice", m["name"])
	require.Equal(t, int64(30), m["age"])
}

func TestCtyToInterface_Tuple(t *testing.T) {
	val := cty.TupleVal([]cty.Value{
		cty.StringVal("a"),
		cty.StringVal("b"),
		cty.NumberIntVal(3),
	})
	result := CtyToInterface(val)
	list, ok := result.([]any)
	require.True(t, ok)
	require.Len(t, list, 3)
	require.Equal(t, "a", list[0])
	require.Equal(t, "b", list[1])
	require.Equal(t, int64(3), list[2])
}

func TestCtyToInterface_List(t *testing.T) {
	val := cty.ListVal([]cty.Value{
		cty.StringVal("x"),
		cty.StringVal("y"),
	})
	result := CtyToInterface(val)
	list, ok := result.([]any)
	require.True(t, ok)
	require.Len(t, list, 2)
	require.Equal(t, "x", list[0])
	require.Equal(t, "y", list[1])
}

func TestCtyToInterface_Map(t *testing.T) {
	val := cty.MapVal(map[string]cty.Value{
		"key1": cty.StringVal("val1"),
		"key2": cty.StringVal("val2"),
	})
	result := CtyToInterface(val)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "val1", m["key1"])
	require.Equal(t, "val2", m["key2"])
}

func TestCtyToInterface_NestedObject(t *testing.T) {
	val := cty.ObjectVal(map[string]cty.Value{
		"user": cty.ObjectVal(map[string]cty.Value{
			"name": cty.StringVal("bob"),
			"tags": cty.TupleVal([]cty.Value{
				cty.StringVal("admin"),
				cty.StringVal("active"),
			}),
		}),
		"count": cty.NumberIntVal(1),
	})
	result := CtyToInterface(val)
	m, ok := result.(map[string]any)
	require.True(t, ok)

	user, ok := m["user"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "bob", user["name"])

	tags, ok := user["tags"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"admin", "active"}, tags)

	require.Equal(t, int64(1), m["count"])
}

func TestBuildCommand_RootNameAndDescription(t *testing.T) {
	cfg := &config.CLIConfig{
		Name:        "mytool",
		Description: "My tool description",
	}
	cmd := BuildCommand(cfg)
	require.Equal(t, "mytool", cmd.Use)
	require.Equal(t, "My tool description", cmd.Short)
}

func TestBuildCommand_PersistentFlags(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Flags: []*config.CLIFlagConfig{
			{
				Name:        "server",
				Short:       "s",
				Default:     "http://localhost:8080",
				Description: "Server address",
			},
			{
				Name:        "token",
				Default:     "",
				Description: "Auth token",
			},
		},
	}
	cmd := BuildCommand(cfg)

	serverFlag := cmd.PersistentFlags().Lookup("server")
	require.NotNil(t, serverFlag)
	require.Equal(t, "s", serverFlag.Shorthand)
	require.Equal(t, "http://localhost:8080", serverFlag.DefValue)
	require.Equal(t, "Server address", serverFlag.Usage)

	tokenFlag := cmd.PersistentFlags().Lookup("token")
	require.NotNil(t, tokenFlag)
	require.Equal(t, "", tokenFlag.Shorthand)
	require.Equal(t, "", tokenFlag.DefValue)
}

func TestBuildCommand_EnvVarOverridesFlagDefault(t *testing.T) {
	t.Setenv("TEST_SERVER_ADDR", "http://envhost:9090")

	cfg := &config.CLIConfig{
		Name: "mytool",
		Flags: []*config.CLIFlagConfig{
			{
				Name:    "server",
				Default: "http://localhost:8080",
				Env:     "TEST_SERVER_ADDR",
			},
		},
	}
	cmd := BuildCommand(cfg)

	serverFlag := cmd.PersistentFlags().Lookup("server")
	require.NotNil(t, serverFlag)
	require.Equal(t, "http://envhost:9090", serverFlag.DefValue)
}

func TestBuildCommand_EnvVarNotSet_UsesDefault(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Flags: []*config.CLIFlagConfig{
			{
				Name:    "server",
				Default: "http://localhost:8080",
				Env:     "TEST_UNSET_ENV_VAR_POLYMORPH_12345",
			},
		},
	}
	cmd := BuildCommand(cfg)

	serverFlag := cmd.PersistentFlags().Lookup("server")
	require.NotNil(t, serverFlag)
	require.Equal(t, "http://localhost:8080", serverFlag.DefValue)
}

func TestBuildCommand_Subcommands(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name:        "status",
				Description: "Show status",
			},
			{
				Name:        "version",
				Description: "Show version",
			},
		},
	}
	cmd := BuildCommand(cfg)

	statusCmd, _, err := cmd.Find([]string{"status"})
	require.NoError(t, err)
	require.Equal(t, "status", statusCmd.Name())
	require.Equal(t, "Show status", statusCmd.Short)

	versionCmd, _, err := cmd.Find([]string{"version"})
	require.NoError(t, err)
	require.Equal(t, "version", versionCmd.Name())
}

func TestBuildCommand_NestedSubcommands(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name:        "kv",
				Description: "Key-value commands",
				Commands: []*config.CLICommandConfig{
					{
						Name:        "get",
						Description: "Get a value",
						Args: []*config.CLIArgConfig{
							{Name: "key", Required: true},
						},
						Action: &config.CLIActionConfig{},
					},
					{
						Name:        "list",
						Description: "List values",
						Action:      &config.CLIActionConfig{},
					},
				},
			},
		},
	}
	cmd := BuildCommand(cfg)

	kvCmd, _, err := cmd.Find([]string{"kv"})
	require.NoError(t, err)
	require.Equal(t, "kv", kvCmd.Name())

	getCmd, _, err := cmd.Find([]string{"kv", "get"})
	require.NoError(t, err)
	require.Equal(t, "get", getCmd.Name())
	require.Equal(t, "Get a value", getCmd.Short)
	require.Contains(t, getCmd.Use, "<key>")

	listCmd, _, err := cmd.Find([]string{"kv", "list"})
	require.NoError(t, err)
	require.Equal(t, "list", listCmd.Name())
}

func TestBuildCommand_RequiredArgsValidation(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name: "get",
				Args: []*config.CLIArgConfig{
					{Name: "key", Required: true},
				},
				Action: &config.CLIActionConfig{},
			},
		},
	}
	cmd := BuildCommand(cfg)

	getCmd, _, err := cmd.Find([]string{"get"})
	require.NoError(t, err)
	require.NotNil(t, getCmd.Args)

	err = getCmd.Args(getCmd, []string{})
	require.Error(t, err)

	err = getCmd.Args(getCmd, []string{"mykey"})
	require.NoError(t, err)

	err = getCmd.Args(getCmd, []string{"a", "b"})
	require.Error(t, err)
}

func TestBuildCommand_MixedRequiredOptionalArgs(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name: "copy",
				Args: []*config.CLIArgConfig{
					{Name: "source", Required: true},
					{Name: "dest", Required: false},
				},
				Action: &config.CLIActionConfig{},
			},
		},
	}
	cmd := BuildCommand(cfg)

	copyCmd, _, err := cmd.Find([]string{"copy"})
	require.NoError(t, err)
	require.NotNil(t, copyCmd.Args)

	err = copyCmd.Args(copyCmd, []string{})
	require.Error(t, err)

	err = copyCmd.Args(copyCmd, []string{"src"})
	require.NoError(t, err)

	err = copyCmd.Args(copyCmd, []string{"src", "dst"})
	require.NoError(t, err)
}

func TestBuildCommand_UseStringIncludesArgs(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name: "get",
				Args: []*config.CLIArgConfig{
					{Name: "key", Required: true},
					{Name: "format", Required: false},
				},
				Action: &config.CLIActionConfig{},
			},
		},
	}
	cmd := BuildCommand(cfg)

	getCmd, _, err := cmd.Find([]string{"get"})
	require.NoError(t, err)
	require.Equal(t, "get <key> [format]", getCmd.Use)
}

func TestBuildCommand_CommandLevelFlags(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name: "list",
				Flags: []*config.CLIFlagConfig{
					{
						Name:        "format",
						Short:       "f",
						Default:     "json",
						Description: "Output format",
					},
				},
				Action: &config.CLIActionConfig{},
			},
		},
	}
	cmd := BuildCommand(cfg)

	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)

	formatFlag := listCmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag)
	require.Equal(t, "f", formatFlag.Shorthand)
	require.Equal(t, "json", formatFlag.DefValue)
	require.Equal(t, "Output format", formatFlag.Usage)

	rootFlag := cmd.PersistentFlags().Lookup("format")
	require.Nil(t, rootFlag)
}

func TestBuildCommand_NoActionNoRunE(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name:        "kv",
				Description: "Parent command with no action",
				Commands: []*config.CLICommandConfig{
					{
						Name:   "get",
						Action: &config.CLIActionConfig{},
					},
				},
			},
		},
	}
	cmd := BuildCommand(cfg)

	kvCmd, _, err := cmd.Find([]string{"kv"})
	require.NoError(t, err)
	require.Nil(t, kvCmd.RunE)

	getCmd, _, err := cmd.Find([]string{"kv", "get"})
	require.NoError(t, err)
	require.NotNil(t, getCmd.RunE)
}

func TestBuildCommand_NoArgsOnCommandWithoutAction(t *testing.T) {
	cfg := &config.CLIConfig{
		Name: "mytool",
		Commands: []*config.CLICommandConfig{
			{
				Name: "parent",
				Args: []*config.CLIArgConfig{
					{Name: "thing", Required: true},
				},
			},
		},
	}
	cmd := BuildCommand(cfg)

	parentCmd, _, err := cmd.Find([]string{"parent"})
	require.NoError(t, err)
	require.Equal(t, "parent", parentCmd.Use)
}
