package step

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/norncorp/loki/internal/config"
	"github.com/zclconf/go-cty/cty"
)

// executeHTTPStep executes an HTTP step
func executeHTTPStep(ctx context.Context, httpCfg *config.HTTPStepConfig, evalCtx *hcl.EvalContext) (*Result, error) {
	// Evaluate URL expression
	url, err := evaluateExpressionToString(httpCfg.URLExpr, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate URL: %w", err)
	}

	// Evaluate method (default to GET)
	method := "GET"
	if httpCfg.Method != "" {
		method = httpCfg.Method
	}

	// Evaluate body if present
	var bodyReader io.Reader
	if httpCfg.BodyExpr != nil {
		bodyStr, err := evaluateExpressionToString(httpCfg.BodyExpr, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate body: %w", err)
		}
		bodyReader = strings.NewReader(bodyStr)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Evaluate and add headers if present
	if httpCfg.HeadersExpr != nil {
		headers, err := evaluateHeaders(httpCfg.HeadersExpr, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate headers: %w", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	// Execute request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Try to parse as JSON
	var body interface{}
	if len(bodyBytes) > 0 && resp.Header.Get("Content-Type") != "" {
		if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				// If JSON parsing fails, use raw string
				body = string(bodyBytes)
			}
		} else {
			body = string(bodyBytes)
		}
	} else if len(bodyBytes) > 0 {
		// No content-type, try JSON anyway
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			body = string(bodyBytes)
		}
	}

	// Collect response headers
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	return &Result{
		Body:    body,
		Status:  resp.StatusCode,
		Headers: headers,
	}, nil
}

// evaluateExpressionToString evaluates an HCL expression to a string
func evaluateExpressionToString(expr hcl.Expression, evalCtx *hcl.EvalContext) (string, error) {
	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return "", fmt.Errorf("failed to evaluate expression: %s", diags.Error())
	}

	// Convert to string
	if value.Type().Equals(cty.String) {
		return value.AsString(), nil
	}

	return "", fmt.Errorf("expression did not evaluate to string, got %s", value.Type().FriendlyName())
}

// evaluateHeaders evaluates an HCL expression to a map of headers
func evaluateHeaders(expr hcl.Expression, evalCtx *hcl.EvalContext) (map[string]string, error) {
	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate expression: %s", diags.Error())
	}

	// Headers should be an object/map
	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("headers must be an object/map, got %s", value.Type().FriendlyName())
	}

	headers := make(map[string]string)
	for key, val := range value.AsValueMap() {
		if !val.Type().Equals(cty.String) {
			return nil, fmt.Errorf("header %q must be a string, got %s", key, val.Type().FriendlyName())
		}
		headers[key] = val.AsString()
	}

	return headers, nil
}

// evaluateString evaluates an HCL expression string
// If the string doesn't contain interpolation, it's returned as-is
func evaluateString(expr string, evalCtx *hcl.EvalContext) (string, error) {
	// Check if this looks like a template with interpolation
	// If not, return it as-is
	if !strings.Contains(expr, "${") {
		return expr, nil
	}

	// Parse as a template expression
	parsedExpr, diags := hclsyntax.ParseTemplate([]byte(expr), "inline", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return "", fmt.Errorf("failed to parse template: %s", diags.Error())
	}

	// Evaluate the expression
	value, diags := parsedExpr.Value(evalCtx)
	if diags.HasErrors() {
		return "", fmt.Errorf("failed to evaluate template: %s", diags.Error())
	}

	// Convert to string
	if value.Type().Equals(cty.String) {
		return value.AsString(), nil
	}

	return "", fmt.Errorf("template did not evaluate to string")
}

// evaluateExpressionToBytes evaluates an HCL expression and converts it to bytes
func evaluateExpressionToBytes(expr hcl.Expression, evalCtx *hcl.EvalContext) ([]byte, error) {
	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate expression: %s", diags.Error())
	}

	// If it's a string, use it directly
	if value.Type().Equals(cty.String) {
		return []byte(value.AsString()), nil
	}

	// Otherwise, try to JSON encode it
	var result interface{}
	err := unmarshalCty(value, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value: %w", err)
	}

	return json.Marshal(result)
}

// unmarshalCty converts a cty.Value to a Go interface{}
func unmarshalCty(val cty.Value, target *interface{}) error {
	if val.IsNull() {
		*target = nil
		return nil
	}

	switch {
	case val.Type().Equals(cty.Bool):
		*target = val.True()
	case val.Type().Equals(cty.Number):
		f := val.AsBigFloat()
		if f.IsInt() {
			i, _ := f.Int64()
			*target = i
		} else {
			fl, _ := f.Float64()
			*target = fl
		}
	case val.Type().Equals(cty.String):
		*target = val.AsString()
	default:
		return fmt.Errorf("unsupported type: %s", val.Type().FriendlyName())
	}

	return nil
}
