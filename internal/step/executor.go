package step

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	"github.com/zclconf/go-cty/cty"
)

// Result contains the output from a step execution
type Result struct {
	Body   interface{}       // Parsed response body
	Status int               // HTTP status code (for HTTP steps)
	Headers map[string]string // Response headers (for HTTP steps)
	Error  error             // Error if step failed
}

// Executor executes steps and builds context for expression evaluation
type Executor struct {
	steps   []*config.StepConfig
	results map[string]*Result
}

// NewExecutor creates a new step executor
func NewExecutor(steps []*config.StepConfig) *Executor {
	return &Executor{
		steps:   steps,
		results: make(map[string]*Result),
	}
}

// Execute runs all steps in order, building up context for subsequent steps
func (e *Executor) Execute(ctx context.Context, evalCtx *hcl.EvalContext) error {
	for _, step := range e.steps {
		// Execute the step based on its type
		result, err := e.executeStep(ctx, step, evalCtx)
		if err != nil {
			return fmt.Errorf("step %q failed: %w", step.Name, err)
		}

		// Store the result
		e.results[step.Name] = result

		// Add step result to evaluation context for subsequent steps
		if evalCtx.Variables == nil {
			evalCtx.Variables = make(map[string]cty.Value)
		}

		// Create step context object
		stepVars := map[string]cty.Value{
			"body":   interfaceToCty(result.Body),
			"status": cty.NumberIntVal(int64(result.Status)),
		}

		// Add step to context
		if _, ok := evalCtx.Variables["step"]; !ok {
			evalCtx.Variables["step"] = cty.ObjectVal(make(map[string]cty.Value))
		}

		// Get existing step map
		stepMap := make(map[string]cty.Value)
		if stepObj, ok := evalCtx.Variables["step"]; ok && stepObj.Type().IsObjectType() {
			for key, val := range stepObj.AsValueMap() {
				stepMap[key] = val
			}
		}

		// Add this step's result
		stepMap[step.Name] = cty.ObjectVal(stepVars)
		evalCtx.Variables["step"] = cty.ObjectVal(stepMap)
	}

	return nil
}

// executeStep executes a single step based on its type
func (e *Executor) executeStep(ctx context.Context, step *config.StepConfig, evalCtx *hcl.EvalContext) (*Result, error) {
	// For now, only HTTP steps are supported
	if step.HTTP != nil {
		return executeHTTPStep(ctx, step.HTTP, evalCtx)
	}

	return nil, fmt.Errorf("unknown step type for step %q", step.Name)
}

// Results returns all step results
func (e *Executor) Results() map[string]*Result {
	return e.results
}

// interfaceToCty converts a Go interface{} to a cty.Value
func interfaceToCty(v interface{}) cty.Value {
	if v == nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	switch val := v.(type) {
	case string:
		return cty.StringVal(val)
	case int:
		return cty.NumberIntVal(int64(val))
	case int64:
		return cty.NumberIntVal(val)
	case float64:
		return cty.NumberFloatVal(val)
	case bool:
		return cty.BoolVal(val)
	case map[string]interface{}:
		m := make(map[string]cty.Value)
		for k, v := range val {
			m[k] = interfaceToCty(v)
		}
		return cty.ObjectVal(m)
	case []interface{}:
		vals := make([]cty.Value, len(val))
		for i, v := range val {
			vals[i] = interfaceToCty(v)
		}
		if len(vals) == 0 {
			return cty.ListValEmpty(cty.DynamicPseudoType)
		}
		return cty.TupleVal(vals)
	default:
		// For unknown types, return string representation
		return cty.StringVal(fmt.Sprintf("%v", v))
	}
}
