package cligen

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// exprContext provides variable name mappings for converting HCL expressions to Go code.
type exprContext struct {
	// FlagVarNames maps flag names to their Go variable names (e.g., "address" -> "flagAddress")
	FlagVarNames map[string]string
	// ArgIndices maps arg names to their positional index (e.g., "path" -> 0)
	ArgIndices map[string]int
	// StepVarNames maps step names to their Go result variable names (e.g., "read" -> "stepReadResult")
	StepVarNames map[string]string
}

// exprToGo converts an HCL expression to Go source code that evaluates to the same value.
// Returns a Go expression string.
func exprToGo(expr hcl.Expression, ctx *exprContext) (string, error) {
	switch e := expr.(type) {
	case *hclsyntax.TemplateExpr:
		return templateExprToGo(e, ctx)

	case *hclsyntax.TemplateWrapExpr:
		return exprToGo(e.Wrapped, ctx)

	case *hclsyntax.LiteralValueExpr:
		return literalToGo(e)

	case *hclsyntax.ScopeTraversalExpr:
		return traversalToGo(e.Traversal, ctx)

	case *hclsyntax.FunctionCallExpr:
		if e.Name == "jsonencode" && len(e.Args) == 1 {
			inner, err := exprToGo(e.Args[0], ctx)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("mustJSON(%s)", inner), nil
		}
		return "", fmt.Errorf("unsupported function call: %s", e.Name)

	case *hclsyntax.ObjectConsExpr:
		return objectConsToGo(e, ctx)

	default:
		return "", fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// templateExprToGo converts a template expression (string interpolation) to Go code.
func templateExprToGo(expr *hclsyntax.TemplateExpr, ctx *exprContext) (string, error) {
	if len(expr.Parts) == 0 {
		return `""`, nil
	}
	if len(expr.Parts) == 1 {
		return exprToGo(expr.Parts[0], ctx)
	}

	// Collect format parts and arguments for fmt.Sprintf
	var formatParts []string
	var args []string

	for _, part := range expr.Parts {
		switch p := part.(type) {
		case *hclsyntax.LiteralValueExpr:
			if p.Val.Type() == cty.String {
				// Escape percent signs in literal strings for Sprintf
				formatParts = append(formatParts, strings.ReplaceAll(p.Val.AsString(), "%", "%%"))
			} else {
				formatParts = append(formatParts, "%v")
				goCode, err := exprToGo(part, ctx)
				if err != nil {
					return "", err
				}
				args = append(args, goCode)
			}
		default:
			formatParts = append(formatParts, "%v")
			goCode, err := exprToGo(part, ctx)
			if err != nil {
				return "", err
			}
			args = append(args, goCode)
		}
	}

	format := strings.Join(formatParts, "")
	if len(args) == 0 {
		return fmt.Sprintf("%q", format), nil
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", format, strings.Join(args, ", ")), nil
}

// literalToGo converts a literal value expression to Go code.
func literalToGo(expr *hclsyntax.LiteralValueExpr) (string, error) {
	switch {
	case expr.Val.Type() == cty.String:
		return fmt.Sprintf("%q", expr.Val.AsString()), nil
	case expr.Val.Type() == cty.Number:
		bf := expr.Val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return fmt.Sprintf("%d", i), nil
		}
		f, _ := bf.Float64()
		return fmt.Sprintf("%g", f), nil
	case expr.Val.Type() == cty.Bool:
		return fmt.Sprintf("%t", expr.Val.True()), nil
	default:
		return "", fmt.Errorf("unsupported literal type: %s", expr.Val.Type().FriendlyName())
	}
}

// traversalToGo converts a scope traversal (e.g., flag.address, step.read.body.data) to Go code.
func traversalToGo(traversal hcl.Traversal, ctx *exprContext) (string, error) {
	if len(traversal) < 2 {
		return "", fmt.Errorf("traversal too short: %s", traversal.RootName())
	}

	root := traversal.RootName()
	attr, ok := traversal[1].(hcl.TraverseAttr)
	if !ok {
		return "", fmt.Errorf("expected attribute traversal after root %q", root)
	}

	switch root {
	case "flag":
		varName, exists := ctx.FlagVarNames[attr.Name]
		if !exists {
			return "", fmt.Errorf("unknown flag %q", attr.Name)
		}
		return varName, nil

	case "arg":
		idx, exists := ctx.ArgIndices[attr.Name]
		if !exists {
			return "", fmt.Errorf("unknown arg %q", attr.Name)
		}
		return fmt.Sprintf("args[%d]", idx), nil

	case "step":
		varName, exists := ctx.StepVarNames[attr.Name]
		if !exists {
			return "", fmt.Errorf("unknown step %q", attr.Name)
		}
		// Build path segments for jsonPath() call
		var pathParts []string
		for i := 2; i < len(traversal); i++ {
			pathAttr, ok := traversal[i].(hcl.TraverseAttr)
			if !ok {
				return "", fmt.Errorf("expected attribute traversal in step path")
			}
			pathParts = append(pathParts, fmt.Sprintf("%q", pathAttr.Name))
		}
		if len(pathParts) > 0 {
			return fmt.Sprintf("jsonPath(%s, %s)", varName, strings.Join(pathParts, ", ")), nil
		}
		return varName, nil

	default:
		return "", fmt.Errorf("unsupported variable root %q (expected flag, arg, or step)", root)
	}
}

// objectConsToGo converts an HCL object constructor to a Go map literal.
func objectConsToGo(expr *hclsyntax.ObjectConsExpr, ctx *exprContext) (string, error) {
	var pairs []string
	for _, item := range expr.Items {
		key, err := exprToGo(item.KeyExpr, ctx)
		if err != nil {
			return "", fmt.Errorf("object key: %w", err)
		}
		val, err := exprToGo(item.ValueExpr, ctx)
		if err != nil {
			return "", fmt.Errorf("object value: %w", err)
		}
		pairs = append(pairs, fmt.Sprintf("%s: %s", key, val))
	}
	return fmt.Sprintf("map[string]any{%s}", strings.Join(pairs, ", ")), nil
}
