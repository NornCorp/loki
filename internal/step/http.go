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
	url, err := evaluateString(httpCfg.URL, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate URL: %w", err)
	}

	// Evaluate method (default to GET)
	method := "GET"
	if httpCfg.Method != "" {
		method, err = evaluateString(httpCfg.Method, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate method: %w", err)
		}
	}

	// Evaluate body if present
	var bodyReader io.Reader
	if httpCfg.Body != "" {
		bodyStr, err := evaluateString(httpCfg.Body, evalCtx)
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

	// Add headers
	for key, valueExpr := range httpCfg.Headers {
		value, err := evaluateString(valueExpr, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate header %q: %w", key, err)
		}
		req.Header.Set(key, value)
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
