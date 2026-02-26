package step

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/norncorp/loki/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

// mustParseExpr parses a string as an HCL expression (for testing)
func mustParseExpr(expr string) hcl.Expression {
	parsed, diags := hclsyntax.ParseExpression([]byte(expr), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		panic(diags.Error())
	}
	return parsed
}

func TestExecutor_ExecuteSteps(t *testing.T) {
	// Create a test HTTP server to act as upstream
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/123":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":   "123",
				"name": "John Doe",
			})
		case "/orders":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "1", "product": "Widget", "total": 29.99},
				{"id": "2", "product": "Gadget", "total": 49.99},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	tests := []struct {
		name    string
		steps   []*config.StepConfig
		wantErr bool
		check   func(t *testing.T, results map[string]*Result)
	}{
		{
			name: "single HTTP step",
			steps: []*config.StepConfig{
				{
					Name: "user",
					HTTP: &config.HTTPStepConfig{
						URLExpr: mustParseExpr(`"` + upstream.URL + `/users/123"`),
						Method:  "GET",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, results map[string]*Result) {
				require.Contains(t, results, "user")
				require.Equal(t, 200, results["user"].Status)
				require.NotNil(t, results["user"].Body)

				// Check body is parsed as JSON
				body, ok := results["user"].Body.(map[string]any)
				require.True(t, ok, "expected body to be map[string]any")
				require.Equal(t, "123", body["id"])
				require.Equal(t, "John Doe", body["name"])
			},
		},
		{
			name: "multiple HTTP steps",
			steps: []*config.StepConfig{
				{
					Name: "user",
					HTTP: &config.HTTPStepConfig{
						URLExpr: mustParseExpr(`"` + upstream.URL + `/users/123"`),
						Method:  "GET",
					},
				},
				{
					Name: "orders",
					HTTP: &config.HTTPStepConfig{
						URLExpr: mustParseExpr(`"` + upstream.URL + `/orders"`),
						Method:  "GET",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, results map[string]*Result) {
				require.Contains(t, results, "user")
				require.Contains(t, results, "orders")

				require.Equal(t, 200, results["user"].Status)
				require.Equal(t, 200, results["orders"].Status)

				// Check both bodies are present
				require.NotNil(t, results["user"].Body)
				require.NotNil(t, results["orders"].Body)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewExecutor(tt.steps)
			evalCtx := &hcl.EvalContext{
				Variables: make(map[string]cty.Value),
				Functions: config.Functions(),
			}

			err := executor.Execute(context.Background(), evalCtx)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, executor.Results())
			}
		})
	}
}

func TestExecutor_StepContextAvailability(t *testing.T) {
	// Create a test HTTP server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"value": 42})
	}))
	defer upstream.Close()

	steps := []*config.StepConfig{
		{
			Name: "first",
			HTTP: &config.HTTPStepConfig{
				URLExpr: mustParseExpr(`"` + upstream.URL + `/data"`),
				Method:  "GET",
			},
		},
	}

	executor := NewExecutor(steps)
	evalCtx := &hcl.EvalContext{
		Variables: make(map[string]cty.Value),
		Functions: config.Functions(),
	}

	err := executor.Execute(context.Background(), evalCtx)
	require.NoError(t, err)

	// Verify step result is available in context
	require.Contains(t, evalCtx.Variables, "step")

	stepVar := evalCtx.Variables["step"]
	require.True(t, stepVar.Type().IsObjectType())

	// Check that step.first exists
	stepMap := stepVar.AsValueMap()
	require.Contains(t, stepMap, "first")

	// Check that step.first has body and status
	firstStep := stepMap["first"]
	require.True(t, firstStep.Type().IsObjectType())

	firstMap := firstStep.AsValueMap()
	require.Contains(t, firstMap, "body")
	require.Contains(t, firstMap, "status")

	// Verify status is 200
	statusInt, _ := firstMap["status"].AsBigFloat().Int64()
	require.Equal(t, int64(200), statusInt)
}
