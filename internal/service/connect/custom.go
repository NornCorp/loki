package connect

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"connectrpc.com/connect"
	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	configconnect "github.com/norncorp/loki/internal/config/connect"
	"github.com/norncorp/loki/internal/step"
	"github.com/zclconf/go-cty/cty"
)

// CustomMethodHandler handles custom Connect-RPC methods with steps
type CustomMethodHandler struct {
	method      *configconnect.Handler
	packageName string
	serviceName string
	serviceVars map[string]cty.Value
}

// NewCustomMethodHandler creates a new custom method handler
func NewCustomMethodHandler(method *configconnect.Handler, packageName, serviceName string, serviceVars map[string]cty.Value) (*CustomMethodHandler, error) {
	return &CustomMethodHandler{
		method:      method,
		packageName: packageName,
		serviceName: serviceName,
		serviceVars: serviceVars,
	}, nil
}

// RegisterHandler registers this custom method and returns the path and handler function
func (h *CustomMethodHandler) RegisterHandler() (string, http.HandlerFunc) {
	// Create the method path: /api.v1.UserService/MethodName
	methodPath := fmt.Sprintf("/%s.%s/%s", h.packageName, h.serviceName, h.method.Name)

	return methodPath, h.handleMethod
}

// handleMethod handles the custom RPC method
func (h *CustomMethodHandler) handleMethod(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}

	// Build evaluation context from request
	evalCtx := buildEvalContext(req, h.serviceVars)

	// Execute steps if present
	if len(h.method.Steps) > 0 {
		executor := step.NewExecutor(h.method.Steps)
		if err := executor.Execute(r.Context(), evalCtx); err != nil {
			h.writeError(w, connect.NewError(connect.CodeInternal, fmt.Errorf("step execution failed: %w", err)))
			return
		}
	}

	// Evaluate response body expression if present
	var response any
	if h.method.Response != nil && h.method.Response.BodyExpr != nil {
		value, diags := h.method.Response.BodyExpr.Value(evalCtx)
		if diags.HasErrors() {
			h.writeError(w, connect.NewError(connect.CodeInternal, fmt.Errorf("response evaluation failed: %s", diags.Error())))
			return
		}

		// Parse the response body as JSON
		bodyStr := value.AsString()
		if err := json.Unmarshal([]byte(bodyStr), &response); err != nil {
			// If it's not JSON, return as string
			response = map[string]any{"result": bodyStr}
		}
	} else {
		// Empty response
		response = map[string]any{}
	}

	// Write response
	h.writeResponse(w, response)
}

// writeResponse writes a successful Connect-RPC response
func (h *CustomMethodHandler) writeResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	data, err := json.Marshal(resp)
	if err != nil {
		h.writeError(w, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal response: %w", err)))
		return
	}

	w.Write(data)
}

// writeError writes a Connect-RPC error response
func (h *CustomMethodHandler) writeError(w http.ResponseWriter, err *connect.Error) {
	w.Header().Set("Content-Type", "application/json")

	// Map Connect code to HTTP status code
	httpStatus := http.StatusInternalServerError
	switch err.Code() {
	case connect.CodeInvalidArgument:
		httpStatus = http.StatusBadRequest
	case connect.CodeNotFound:
		httpStatus = http.StatusNotFound
	case connect.CodeAlreadyExists:
		httpStatus = http.StatusConflict
	case connect.CodePermissionDenied:
		httpStatus = http.StatusForbidden
	case connect.CodeUnauthenticated:
		httpStatus = http.StatusUnauthorized
	}

	w.WriteHeader(httpStatus)

	// Write error in Connect format
	errResp := map[string]any{
		"code":    err.Code().String(),
		"message": err.Message(),
	}

	data, _ := json.Marshal(errResp)
	w.Write(data)
}

// buildEvalContext builds an HCL evaluation context from the request
func buildEvalContext(req map[string]any, serviceVars map[string]cty.Value) *hcl.EvalContext {
	return config.BuildEvalContextFromMap(req, serviceVars)
}
