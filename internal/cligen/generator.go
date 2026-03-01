package cligen

import (
	"bytes"
	"fmt"
	"go/format"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jumppad-labs/polymorph/internal/config"
)

// Generate creates a standalone Go binary from a CLIConfig.
// It generates Go source code, writes it to a temp directory, and compiles it.
func Generate(cfg *config.CLIConfig, outputPath string) error {
	src, err := GenerateSource(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate source: %w", err)
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "polymorph-cligen-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write main.go
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), src, 0644); err != nil {
		return fmt.Errorf("failed to write main.go: %w", err)
	}

	// Detect Go version
	goVersion := detectGoVersion()

	// Write go.mod
	goMod := fmt.Sprintf("module generated-%s\n\ngo %s\n\nrequire github.com/spf13/cobra v1.8.0\n", cfg.Name, goVersion)
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	// Run go mod tidy
	slog.Info("resolving dependencies")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy failed: %s\n%w", string(out), err)
	}

	// Resolve absolute output path
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	// Build binary
	slog.Info("compiling", "name", cfg.Name)
	buildCmd := exec.Command("go", "build", "-o", absOutput, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %s\n%w", string(out), err)
	}

	return nil
}

// GenerateSource generates the Go source code for a CLI binary.
// Exported for testing.
func GenerateSource(cfg *config.CLIConfig) ([]byte, error) {
	g := &generator{
		cfg:          cfg,
		buf:          &bytes.Buffer{},
		flagVarNames: make(map[string]string),
		imports: map[string]bool{
			"fmt":                       true,
			"os":                        true,
			"github.com/spf13/cobra":    true,
		},
	}

	if err := g.generate(); err != nil {
		return nil, err
	}

	// Format source
	formatted, err := format.Source(g.buf.Bytes())
	if err != nil {
		// Return unformatted source for debugging
		return g.buf.Bytes(), fmt.Errorf("failed to format source (returning raw): %w", err)
	}

	return formatted, nil
}

type generator struct {
	cfg          *config.CLIConfig
	buf          *bytes.Buffer
	flagVarNames map[string]string // global flag name -> Go var name
	imports      map[string]bool
}

func (g *generator) generate() error {
	// First pass: collect what we need (imports, flag names)
	g.collectFlags(g.cfg.Flags, "")
	g.collectCommandNeeds(g.cfg.Commands)

	// Generate source
	g.emit("package main\n\n")
	g.emitImports()
	g.emit("\nfunc main() {\n")
	g.emit("\trootCmd := &cobra.Command{\n")
	g.emit("\t\tUse:   %q,\n", g.cfg.Name)
	if g.cfg.Description != "" {
		g.emit("\t\tShort: %q,\n", g.cfg.Description)
	}
	g.emit("\t}\n\n")

	// Emit global flag declarations
	for _, f := range g.cfg.Flags {
		g.emitFlagDecl(f, "rootCmd", true)
	}

	// Emit commands
	for _, cmd := range g.cfg.Commands {
		if err := g.emitCommand(cmd, "rootCmd"); err != nil {
			return err
		}
	}

	g.emit("\tif err := rootCmd.Execute(); err != nil {\n")
	g.emit("\t\tos.Exit(1)\n")
	g.emit("\t}\n")
	g.emit("}\n")

	// Emit helper functions
	g.emitHelpers()

	return nil
}

// collectFlags maps flag names to Go variable names.
func (g *generator) collectFlags(flags []*config.CLIFlagConfig, prefix string) {
	for _, f := range flags {
		varName := "flag" + toCamelCase(prefix+f.Name)
		g.flagVarNames[f.Name] = varName
	}
}

// collectCommandNeeds scans commands for features that require imports.
func (g *generator) collectCommandNeeds(cmds []*config.CLICommandConfig) {
	for _, cmd := range cmds {
		g.collectFlags(cmd.Flags, cmd.Name)
		if cmd.Action != nil {
			if len(cmd.Action.Steps) > 0 {
				g.imports["encoding/json"] = true
				g.imports["io"] = true
				g.imports["net/http"] = true
				g.imports["strings"] = true
			}
			if cmd.Action.Output != nil {
				g.imports["encoding/json"] = true
				if cmd.Action.Output.Format == "table" {
					g.imports["strings"] = true
					g.imports["text/tabwriter"] = true
				}
			}
		}
		g.collectCommandNeeds(cmd.Commands)
	}
}

func (g *generator) emitImports() {
	g.emit("import (\n")
	// Stdlib imports first
	stdlibs := []string{"encoding/json", "fmt", "io", "net/http", "os", "strings", "text/tabwriter"}
	for _, imp := range stdlibs {
		if g.imports[imp] {
			g.emit("\t%q\n", imp)
		}
	}
	g.emit("\n")
	// Third-party
	for imp := range g.imports {
		if !isStdlib(imp) {
			g.emit("\t%q\n", imp)
		}
	}
	g.emit(")\n")
}

func (g *generator) emitFlagDecl(f *config.CLIFlagConfig, cmdVar string, persistent bool) {
	varName := g.flagVarNames[f.Name]
	g.emit("\tvar %s string\n", varName)

	flagMethod := "Flags"
	if persistent {
		flagMethod = "PersistentFlags"
	}

	if f.Short != "" {
		if f.Env != "" {
			g.emit("\t%s.%s().StringVarP(&%s, %q, %q, envOr(%q, %q), %q)\n",
				cmdVar, flagMethod, varName, f.Name, f.Short, f.Env, f.Default, f.Description)
		} else {
			g.emit("\t%s.%s().StringVarP(&%s, %q, %q, %q, %q)\n",
				cmdVar, flagMethod, varName, f.Name, f.Short, f.Default, f.Description)
		}
	} else {
		if f.Env != "" {
			g.emit("\t%s.%s().StringVar(&%s, %q, envOr(%q, %q), %q)\n",
				cmdVar, flagMethod, varName, f.Name, f.Env, f.Default, f.Description)
		} else {
			g.emit("\t%s.%s().StringVar(&%s, %q, %q, %q)\n",
				cmdVar, flagMethod, varName, f.Name, f.Default, f.Description)
		}
	}

	if f.Required {
		g.emit("\t%s.MarkFlagRequired(%q)\n", cmdVar, f.Name)
	}
	g.emit("\n")
}

func (g *generator) emitCommand(cmd *config.CLICommandConfig, parentVar string) error {
	cmdVar := "cmd" + toCamelCase(cmd.Name)

	// Build Use string with args
	use := cmd.Name
	if cmd.Action != nil && len(cmd.Args) > 0 {
		var argParts []string
		for _, a := range cmd.Args {
			if a.Required {
				argParts = append(argParts, "<"+a.Name+">")
			} else {
				argParts = append(argParts, "["+a.Name+"]")
			}
		}
		use += " " + strings.Join(argParts, " ")
	}

	g.emit("\t%s := &cobra.Command{\n", cmdVar)
	g.emit("\t\tUse:   %q,\n", use)
	if cmd.Description != "" {
		g.emit("\t\tShort: %q,\n", cmd.Description)
	}

	// Args validation
	if cmd.Action != nil && len(cmd.Args) > 0 {
		var requiredCount int
		for _, a := range cmd.Args {
			if a.Required {
				requiredCount++
			}
		}
		if requiredCount == len(cmd.Args) {
			g.emit("\t\tArgs: cobra.ExactArgs(%d),\n", len(cmd.Args))
		} else {
			g.emit("\t\tArgs: cobra.MinimumNArgs(%d),\n", requiredCount)
		}
	}

	// RunE for leaf commands with actions
	if cmd.Action != nil {
		body, err := g.generateActionBody(cmd)
		if err != nil {
			return fmt.Errorf("command %q: %w", cmd.Name, err)
		}
		g.emit("\t\tRunE: func(cmd *cobra.Command, args []string) error {\n")
		g.emit("%s", body)
		g.emit("\t\t},\n")
	}

	g.emit("\t}\n")

	// Command-level flags
	for _, f := range cmd.Flags {
		g.emitFlagDecl(f, cmdVar, false)
	}

	// Subcommands
	for _, sub := range cmd.Commands {
		if err := g.emitCommand(sub, cmdVar); err != nil {
			return err
		}
	}

	g.emit("\t%s.AddCommand(%s)\n\n", parentVar, cmdVar)

	return nil
}

func (g *generator) generateActionBody(cmd *config.CLICommandConfig) (string, error) {
	var buf bytes.Buffer

	// Build expression context
	ctx := &exprContext{
		FlagVarNames: g.flagVarNames,
		ArgIndices:   make(map[string]int),
		StepVarNames: make(map[string]string),
	}

	// Map arg names to indices
	for i, a := range cmd.Args {
		ctx.ArgIndices[a.Name] = i
	}

	// Map step names to var names
	for _, step := range cmd.Action.Steps {
		varName := "step" + toCamelCase(step.Name) + "Result"
		ctx.StepVarNames[step.Name] = varName
	}

	// Generate step executions
	for _, step := range cmd.Action.Steps {
		if step.HTTP == nil {
			continue
		}

		varName := ctx.StepVarNames[step.Name]

		// Convert URL expression
		urlCode, err := exprToGo(step.HTTP.URLExpr, ctx)
		if err != nil {
			return "", fmt.Errorf("step %q URL: %w", step.Name, err)
		}

		method := step.HTTP.Method
		if method == "" {
			method = "GET"
		}

		// Convert headers if present
		headersCode := "nil"
		if step.HTTP.HeadersExpr != nil {
			hCode, err := exprToGo(step.HTTP.HeadersExpr, ctx)
			if err == nil && hCode != "" {
				headersCode = hCode
			}
		}

		// Convert body if present
		bodyCode := "nil"
		if step.HTTP.BodyExpr != nil {
			bCode, err := exprToGo(step.HTTP.BodyExpr, ctx)
			if err == nil && bCode != "" {
				bodyCode = fmt.Sprintf("strBody(%s)", bCode)
			}
		}

		fmt.Fprintf(&buf, "\t\t\t%s, err := httpStep(%q, %s, %s, %s)\n",
			varName, method, urlCode, headersCode, bodyCode)
		fmt.Fprintf(&buf, "\t\t\tif err != nil {\n")
		fmt.Fprintf(&buf, "\t\t\t\treturn fmt.Errorf(\"step %%q failed: %%w\", %q, err)\n", step.Name)
		fmt.Fprintf(&buf, "\t\t\t}\n\n")
	}

	// Generate output
	if cmd.Action.Output != nil {
		output := cmd.Action.Output
		outputFormat := output.Format
		if outputFormat == "" {
			outputFormat = "json"
		}

		// Determine what to output
		dataCode := "nil"
		if output.DataExpr != nil {
			code, err := exprToGo(output.DataExpr, ctx)
			if err != nil {
				return "", fmt.Errorf("output data: %w", err)
			}
			dataCode = code
		} else if len(ctx.StepVarNames) > 0 {
			// Default to last step result
			for _, step := range cmd.Action.Steps {
				dataCode = ctx.StepVarNames[step.Name]
			}
		}

		switch outputFormat {
		case "json":
			fmt.Fprintf(&buf, "\t\t\treturn outputJSON(%s)\n", dataCode)
		case "table":
			if len(output.Columns) > 0 {
				cols := make([]string, len(output.Columns))
				for i, c := range output.Columns {
					cols[i] = fmt.Sprintf("%q", c)
				}
				fmt.Fprintf(&buf, "\t\t\treturn outputTable(%s, []string{%s})\n", dataCode, strings.Join(cols, ", "))
			} else {
				fmt.Fprintf(&buf, "\t\t\treturn outputTable(%s, nil)\n", dataCode)
			}
		case "text":
			fmt.Fprintf(&buf, "\t\t\treturn outputText(%s)\n", dataCode)
		default:
			fmt.Fprintf(&buf, "\t\t\treturn outputJSON(%s)\n", dataCode)
		}
	} else {
		fmt.Fprintf(&buf, "\t\t\treturn nil\n")
	}

	return buf.String(), nil
}

func (g *generator) emitHelpers() {
	// envOr helper
	if g.hasEnvFlags() {
		g.emit(`
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
`)
	}

	// httpStep helper
	if g.imports["net/http"] {
		g.emit(`
func httpStep(method, url string, headers map[string]any, body io.Reader) (map[string]any, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(fmt.Sprintf("%%s", k), fmt.Sprintf("%%v", v))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %%d: %%s", resp.StatusCode, string(data))
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		parsed = string(data)
	}
	return map[string]any{
		"body":   parsed,
		"status": resp.StatusCode,
	}, nil
}
`)

		g.emit(`
func strBody(v any) io.Reader {
	switch val := v.(type) {
	case string:
		return strings.NewReader(val)
	default:
		data, _ := json.Marshal(val)
		return strings.NewReader(string(data))
	}
}
`)
		g.imports["strings"] = true
	}

	// jsonPath helper
	g.emit(`
func jsonPath(data any, path ...string) any {
	current := data
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[key]
	}
	return current
}
`)

	// mustJSON helper
	g.emit(`
func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%%v", v)
	}
	return string(data)
}
`)

	// Output helpers
	if g.imports["encoding/json"] {
		g.emit(`
func outputJSON(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
`)
	}

	if g.imports["text/tabwriter"] {
		g.emit(`
func outputTable(data any, columns []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	items, ok := data.([]any)
	if !ok {
		return fmt.Errorf("table output requires an array")
	}
	if len(items) == 0 {
		return nil
	}
	// Auto-detect columns from first item if not specified
	if len(columns) == 0 {
		if m, ok := items[0].(map[string]any); ok {
			for k := range m {
				columns = append(columns, k)
			}
		}
	}
	// Header
	fmt.Fprintln(w, strings.Join(columns, "\t"))
	// Rows
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var cells []string
		for _, col := range columns {
			cells = append(cells, fmt.Sprintf("%%v", row[col]))
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	return w.Flush()
}
`)
	}

	g.emit(`
func outputText(data any) error {
	fmt.Println(data)
	return nil
}
`)
}

func (g *generator) hasEnvFlags() bool {
	for _, f := range g.cfg.Flags {
		if f.Env != "" {
			return true
		}
	}
	for _, cmd := range g.cfg.Commands {
		if hasEnvFlagsInCmd(cmd) {
			return true
		}
	}
	return false
}

func hasEnvFlagsInCmd(cmd *config.CLICommandConfig) bool {
	for _, f := range cmd.Flags {
		if f.Env != "" {
			return true
		}
	}
	for _, sub := range cmd.Commands {
		if hasEnvFlagsInCmd(sub) {
			return true
		}
	}
	return false
}

func (g *generator) emit(format string, args ...any) {
	fmt.Fprintf(g.buf, format, args...)
}

// toCamelCase converts a snake_case or kebab-case string to CamelCase.
func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(c rune) bool {
		return c == '_' || c == '-' || c == '.'
	})
	var result strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			result.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return result.String()
}

func isStdlib(pkg string) bool {
	return !strings.Contains(pkg, ".")
}

func detectGoVersion() string {
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return "1.22"
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "go")
	// Use just major.minor
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return "1.22"
}
