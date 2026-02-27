package cligen

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/require"
)

func mustParseExpr(t *testing.T, expr string) hcl.Expression {
	t.Helper()
	parsed, diags := hclsyntax.ParseExpression([]byte(expr), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors(), "parse error: %s", diags.Error())
	return parsed
}

func mustParseTemplate(t *testing.T, tmpl string) hcl.Expression {
	t.Helper()
	parsed, diags := hclsyntax.ParseTemplate([]byte(tmpl), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors(), "parse error: %s", diags.Error())
	return parsed
}

func TestExprToGo_LiteralString(t *testing.T) {
	expr := mustParseExpr(t, `"hello world"`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, `"hello world"`, result)
}

func TestExprToGo_FlagReference(t *testing.T) {
	expr := mustParseExpr(t, `flag.address`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{"address": "flagAddress"},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, "flagAddress", result)
}

func TestExprToGo_ArgReference(t *testing.T) {
	expr := mustParseExpr(t, `arg.path`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{"path": 0},
		StepVarNames: map[string]string{},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, "args[0]", result)
}

func TestExprToGo_StepTraversal(t *testing.T) {
	expr := mustParseExpr(t, `step.read.body.data`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{"read": "stepReadResult"},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, `jsonPath(stepReadResult, "body", "data")`, result)
}

func TestExprToGo_StepDeepTraversal(t *testing.T) {
	expr := mustParseExpr(t, `step.list.body.data.keys`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{"list": "stepListResult"},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, `jsonPath(stepListResult, "body", "data", "keys")`, result)
}

func TestExprToGo_TemplateInterpolation(t *testing.T) {
	expr := mustParseTemplate(t, `${flag.address}/v1/secret/data/${arg.path}`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{"address": "flagAddress"},
		ArgIndices:   map[string]int{"path": 0},
		StepVarNames: map[string]string{},
	}
	result, err := exprToGo(expr, ctx)
	require.NoError(t, err)
	require.Equal(t, `fmt.Sprintf("%v/v1/secret/data/%v", flagAddress, args[0])`, result)
}

func TestExprToGo_UnknownFlag(t *testing.T) {
	expr := mustParseExpr(t, `flag.nonexistent`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{},
	}
	_, err := exprToGo(expr, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown flag")
}

func TestExprToGo_UnknownArg(t *testing.T) {
	expr := mustParseExpr(t, `arg.nonexistent`)
	ctx := &exprContext{
		FlagVarNames: map[string]string{},
		ArgIndices:   map[string]int{},
		StepVarNames: map[string]string{},
	}
	_, err := exprToGo(expr, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown arg")
}
