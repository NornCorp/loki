package proxy

import (
	"fmt"
	"net/http"

	"github.com/hashicorp/hcl/v2"
)

// Transform represents request/response transformations
type Transform struct {
	AddHeaders map[string]string
}

// parseHeadersExpr evaluates an HCL expression to a map of header key-value pairs.
// Returns nil if the expression evaluates to null (absent optional attribute).
func parseHeadersExpr(expr hcl.Expression, evalCtx *hcl.EvalContext) (*Transform, error) {
	headersVal, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate headers: %s", diags.Error())
	}

	if headersVal.IsNull() {
		return nil, nil
	}

	xfm := &Transform{
		AddHeaders: make(map[string]string),
	}
	headersMap := headersVal.AsValueMap()
	for k, v := range headersMap {
		xfm.AddHeaders[k] = v.AsString()
	}

	return xfm, nil
}

// ApplyRequest applies transforms to an HTTP request
func (t *Transform) ApplyRequest(req *http.Request) {
	for k, v := range t.AddHeaders {
		req.Header.Set(k, v)
	}
}

// ApplyResponse applies transforms to an HTTP response
func (t *Transform) ApplyResponse(resp *http.Response) {
	for k, v := range t.AddHeaders {
		resp.Header.Set(k, v)
	}
}
