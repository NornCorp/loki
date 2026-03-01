package cligen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hashicorp/hcl/v2"
	"github.com/spf13/cobra"
	"github.com/zclconf/go-cty/cty"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/jumppad-labs/polymorph/internal/step"
)

// BuildCommand creates a cobra command tree from a CLIConfig for runtime execution.
// Instead of generating Go source code, it builds commands dynamically and executes
// steps using the existing step executor.
func BuildCommand(cfg *config.CLIConfig) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          cfg.Name,
		Short:        cfg.Description,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	flagValues := make(map[string]*string)
	registerFlags(rootCmd, cfg.Flags, flagValues, true)

	for _, cmd := range cfg.Commands {
		rootCmd.AddCommand(buildSubcommand(cmd, flagValues))
	}

	return rootCmd
}

func registerFlags(cmd *cobra.Command, flags []*config.CLIFlagConfig, flagValues map[string]*string, persistent bool) {
	for _, f := range flags {
		defaultVal := f.Default
		if f.Env != "" {
			if envVal := os.Getenv(f.Env); envVal != "" {
				defaultVal = envVal
			}
		}

		val := new(string)
		flagValues[f.Name] = val

		if persistent {
			if f.Short != "" {
				cmd.PersistentFlags().StringVarP(val, f.Name, f.Short, defaultVal, f.Description)
			} else {
				cmd.PersistentFlags().StringVar(val, f.Name, defaultVal, f.Description)
			}
			if f.Required {
				cmd.MarkPersistentFlagRequired(f.Name)
			}
		} else {
			if f.Short != "" {
				cmd.Flags().StringVarP(val, f.Name, f.Short, defaultVal, f.Description)
			} else {
				cmd.Flags().StringVar(val, f.Name, defaultVal, f.Description)
			}
			if f.Required {
				cmd.MarkFlagRequired(f.Name)
			}
		}
	}
}

func buildSubcommand(cmdCfg *config.CLICommandConfig, flagValues map[string]*string) *cobra.Command {
	use := cmdCfg.Name
	if cmdCfg.Action != nil && len(cmdCfg.Args) > 0 {
		parts := make([]string, 0, len(cmdCfg.Args)+1)
		parts = append(parts, cmdCfg.Name)
		for _, a := range cmdCfg.Args {
			if a.Required {
				parts = append(parts, "<"+a.Name+">")
			} else {
				parts = append(parts, "["+a.Name+"]")
			}
		}
		use = strings.Join(parts, " ")
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: cmdCfg.Description,
	}

	// Set arg validation for leaf commands
	if cmdCfg.Action != nil {
		requiredArgs := 0
		for _, a := range cmdCfg.Args {
			if a.Required {
				requiredArgs++
			}
		}
		if requiredArgs > 0 && requiredArgs == len(cmdCfg.Args) {
			cmd.Args = cobra.ExactArgs(requiredArgs)
		} else if requiredArgs > 0 {
			cmd.Args = cobra.MinimumNArgs(requiredArgs)
		}
	}

	// Register command-level flags (non-persistent)
	registerFlags(cmd, cmdCfg.Flags, flagValues, false)

	// Set RunE for leaf commands (those with an action)
	if cmdCfg.Action != nil {
		action := cmdCfg.Action
		argConfigs := cmdCfg.Args
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return executeAction(c.Context(), action, argConfigs, flagValues, args)
		}
	}

	// Recursively build subcommands
	for _, sub := range cmdCfg.Commands {
		cmd.AddCommand(buildSubcommand(sub, flagValues))
	}

	return cmd
}

func executeAction(ctx context.Context, action *config.CLIActionConfig, argConfigs []*config.CLIArgConfig, flagValues map[string]*string, args []string) error {
	// Build flag variables
	flagVars := make(map[string]cty.Value)
	for name, ptr := range flagValues {
		flagVars[name] = cty.StringVal(*ptr)
	}

	// Build arg variables
	argVars := make(map[string]cty.Value)
	for i, a := range argConfigs {
		if i < len(args) {
			argVars[a.Name] = cty.StringVal(args[i])
		}
	}

	evalCtx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"step": cty.EmptyObjectVal,
		},
		Functions: config.Functions(),
	}
	if len(flagVars) > 0 {
		evalCtx.Variables["flag"] = cty.ObjectVal(flagVars)
	} else {
		evalCtx.Variables["flag"] = cty.EmptyObjectVal
	}
	if len(argVars) > 0 {
		evalCtx.Variables["arg"] = cty.ObjectVal(argVars)
	} else {
		evalCtx.Variables["arg"] = cty.EmptyObjectVal
	}

	// Execute steps
	executor := step.NewExecutor(action.Steps)
	if err := executor.Execute(ctx, evalCtx); err != nil {
		return err
	}

	// Format and print output
	if action.Output == nil {
		return nil
	}

	return formatOutput(action.Output, evalCtx)
}

func formatOutput(output *config.CLIOutputConfig, evalCtx *hcl.EvalContext) error {
	if output.DataExpr == nil {
		return nil
	}

	val, diags := output.DataExpr.Value(evalCtx)
	if diags.HasErrors() {
		return fmt.Errorf("evaluating output data: %s", diags.Error())
	}

	data := CtyToInterface(val)

	format := output.Format
	if format == "" {
		format = "json"
	}

	switch format {
	case "json":
		return outputJSON(data)
	case "table":
		return outputTable(data, output.Columns)
	case "text":
		return outputText(data)
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
}

// CtyToInterface converts a cty.Value to a Go interface{}.
func CtyToInterface(val cty.Value) any {
	if val.IsNull() {
		return nil
	}

	ty := val.Type()
	switch {
	case ty.Equals(cty.String):
		return val.AsString()
	case ty.Equals(cty.Number):
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case ty.Equals(cty.Bool):
		return val.True()
	case ty.IsObjectType():
		m := make(map[string]any)
		for k, v := range val.AsValueMap() {
			m[k] = CtyToInterface(v)
		}
		return m
	case ty.IsMapType():
		m := make(map[string]any)
		for k, v := range val.AsValueMap() {
			m[k] = CtyToInterface(v)
		}
		return m
	case ty.IsTupleType():
		var result []any
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			result = append(result, CtyToInterface(v))
		}
		return result
	case ty.IsListType(), ty.IsSetType():
		var result []any
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			result = append(result, CtyToInterface(v))
		}
		return result
	default:
		return val.GoString()
	}
}

func outputJSON(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func outputTable(data any, columns []string) error {
	rows, ok := data.([]any)
	if !ok {
		rows = []any{data}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	// Header
	fmt.Fprintln(w, strings.Join(columns, "\t"))

	// Rows
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		vals := make([]string, len(columns))
		for i, col := range columns {
			if v, exists := m[col]; exists {
				vals[i] = fmt.Sprintf("%v", v)
			}
		}
		fmt.Fprintln(w, strings.Join(vals, "\t"))
	}

	return w.Flush()
}

func outputText(data any) error {
	fmt.Println(data)
	return nil
}
