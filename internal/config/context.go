package config

import (
	"net/http"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// BuildEvalContext creates an HCL evaluation context from an HTTP request
// The context includes:
// - request.params - path parameters
// - request.query - query parameters
// - request.body - parsed request body
// - service.<name> - service reference variables (address, host, port, type, url)
// - step.<name> - results from executed steps (added by executor)
func BuildEvalContext(r *http.Request, pathParams map[string]string, serviceVars map[string]cty.Value) *hcl.EvalContext {
	ctx := &hcl.EvalContext{
		Variables: make(map[string]cty.Value),
		Functions: Functions(),
	}

	// Build request context
	requestVars := make(map[string]cty.Value)

	// Add path parameters
	if len(pathParams) > 0 {
		params := make(map[string]cty.Value)
		for k, v := range pathParams {
			params[k] = cty.StringVal(v)
		}
		requestVars["params"] = cty.ObjectVal(params)
	} else {
		requestVars["params"] = cty.EmptyObjectVal
	}

	// Add query parameters
	query := r.URL.Query()
	if len(query) > 0 {
		queryVars := make(map[string]cty.Value)
		for k, values := range query {
			if len(values) > 0 {
				// For simplicity, only use first value
				queryVars[k] = cty.StringVal(values[0])
			}
		}
		requestVars["query"] = cty.ObjectVal(queryVars)
	} else {
		requestVars["query"] = cty.EmptyObjectVal
	}

	// For now, don't parse request body to avoid consuming the reader
	// Future: buffer the body if needed
	requestVars["body"] = cty.NullVal(cty.DynamicPseudoType)

	// Add method and path
	requestVars["method"] = cty.StringVal(r.Method)
	requestVars["path"] = cty.StringVal(r.URL.Path)

	ctx.Variables["request"] = cty.ObjectVal(requestVars)

	// Add service variables if available
	if len(serviceVars) > 0 {
		ctx.Variables["service"] = cty.ObjectVal(serviceVars)
	}

	// Initialize empty step object (will be populated by executor)
	ctx.Variables["step"] = cty.EmptyObjectVal

	return ctx
}

// BuildEvalContextFromMap creates an HCL evaluation context from a map (for RPC requests)
// The context includes:
// - request.<field> - all fields from the request map
// - service.<name> - service reference variables (address, host, port, type, url)
// - step.<name> - results from executed steps (added by executor)
func BuildEvalContextFromMap(reqMap map[string]any, serviceVars map[string]cty.Value) *hcl.EvalContext {
	ctx := &hcl.EvalContext{
		Variables: make(map[string]cty.Value),
		Functions: Functions(),
	}

	// Build request context from map
	requestVars := make(map[string]cty.Value)
	for k, v := range reqMap {
		requestVars[k] = interfaceToCty(v)
	}

	ctx.Variables["request"] = cty.ObjectVal(requestVars)

	// Add service variables if available
	if len(serviceVars) > 0 {
		ctx.Variables["service"] = cty.ObjectVal(serviceVars)
	}

	// Initialize empty step object (will be populated by executor)
	ctx.Variables["step"] = cty.EmptyObjectVal

	return ctx
}

// interfaceToCty converts a Go any to a cty.Value
func interfaceToCty(v any) cty.Value {
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
	case map[string]any:
		m := make(map[string]cty.Value)
		for k, v := range val {
			m[k] = interfaceToCty(v)
		}
		return cty.ObjectVal(m)
	case []any:
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
		return cty.StringVal(strings.TrimSpace(strings.ReplaceAll(string([]byte(v.(string))), "\n", " ")))
	}
}
