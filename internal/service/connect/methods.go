package connect

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/gertd/go-pluralize"
	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/fake"
	"github.com/norncorp/loki/internal/resource"
)

// ResourceHandler handles Connect-RPC CRUD operations for a resource
type ResourceHandler struct {
	resource      *config.ResourceConfig
	store         *resource.Store
	packageName   string
	serviceName   string
	tableName     string
	pluralName    string
	generator     *fake.Generator
	pluralizer    *pluralize.Client
}

// NewResourceHandler creates a new resource handler for Connect-RPC
func NewResourceHandler(res *config.ResourceConfig, store *resource.Store, packageName string) (*ResourceHandler, error) {
	pluralizer := pluralize.NewClient()
	pluralName := pluralizer.Plural(res.Name)

	// Generate service name: UserService for resource "user"
	serviceName := capitalizeFirst(res.Name) + "Service"

	rh := &ResourceHandler{
		resource:     res,
		store:        store,
		packageName:  packageName,
		serviceName:  serviceName,
		tableName:    res.Name,
		pluralName:   pluralName,
		generator:    fake.NewGenerator(),
		pluralizer:   pluralizer,
	}

	return rh, nil
}

// Initialize creates the resource table and generates initial data
func (rh *ResourceHandler) Initialize() error {
	// Build schema from resource config
	fields := make([]resource.Field, 0, len(rh.resource.Fields))
	for _, field := range rh.resource.Fields {
		f := resource.Field{
			Name:       field.Name,
			Type:       mapFieldType(field.Type),
			PrimaryKey: field.Name == "id",
		}
		fields = append(fields, f)
	}

	schema := resource.Schema{
		Name:   rh.tableName,
		Fields: fields,
	}

	// Create table
	if err := rh.store.CreateTable(rh.tableName, schema); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Generate initial data
	rows := 10
	if rh.resource.Rows > 0 {
		rows = rh.resource.Rows
	}

	for i := 0; i < rows; i++ {
		item := make(map[string]any)

		for _, field := range rh.resource.Fields {
			// Build config map from individual fields
			config := make(map[string]any)
			if field.Min != nil {
				config["min"] = *field.Min
			}
			if field.Max != nil {
				config["max"] = *field.Max
			}
			if len(field.Values) > 0 {
				config["values"] = field.Values
			}

			fieldCfg := fake.FieldConfig{
				Name:   field.Name,
				Type:   fake.FakeType(field.Type),
				Config: config,
			}

			value, err := rh.generator.Generate(fieldCfg)
			if err != nil {
				return fmt.Errorf("failed to generate field %q: %w", field.Name, err)
			}

			item[field.Name] = value
		}

		if err := rh.store.Insert(rh.tableName, item); err != nil {
			return fmt.Errorf("failed to insert item: %w", err)
		}
	}

	return nil
}

// RegisterHandlers registers the Connect-RPC handlers and returns the path and handler
func (rh *ResourceHandler) RegisterHandlers() (string, http.Handler) {
	// Create the service path: /api.v1.UserService/
	servicePath := fmt.Sprintf("/%s.%s/", rh.packageName, rh.serviceName)

	// Create a mux for this service's methods
	mux := http.NewServeMux()

	// Register CRUD methods
	// GetUser
	mux.HandleFunc(servicePath+"Get"+capitalizeFirst(rh.resource.Name), rh.handleGet)
	// ListUsers
	mux.HandleFunc(servicePath+"List"+capitalizeFirst(rh.pluralName), rh.handleList)
	// CreateUser
	mux.HandleFunc(servicePath+"Create"+capitalizeFirst(rh.resource.Name), rh.handleCreate)
	// UpdateUser
	mux.HandleFunc(servicePath+"Update"+capitalizeFirst(rh.resource.Name), rh.handleUpdate)
	// DeleteUser
	mux.HandleFunc(servicePath+"Delete"+capitalizeFirst(rh.resource.Name), rh.handleDelete)

	return servicePath, mux
}

// handleGet handles Get<Resource> RPC
func (rh *ResourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	// Parse Connect-RPC request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}

	// Get ID from request
	id, ok := req["id"]
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required")))
		return
	}

	// Get item from store
	item, err := rh.store.Get(rh.tableName, fmt.Sprintf("%v", id))
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeNotFound, err))
		return
	}

	// Write response
	rh.writeResponse(w, item)
}

// handleList handles List<Resources> RPC
func (rh *ResourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	// List all items
	items, err := rh.store.List(rh.tableName)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInternal, err))
		return
	}

	// Create response with items field
	resp := map[string]any{
		rh.pluralName: items,
	}

	// Write response
	rh.writeResponse(w, resp)
}

// handleCreate handles Create<Resource> RPC
func (rh *ResourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}

	// Get resource data from request
	resourceData, ok := req[rh.resource.Name]
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s is required", rh.resource.Name)))
		return
	}

	// Convert to map
	item, ok := resourceData.(map[string]any)
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid %s data", rh.resource.Name)))
		return
	}

	// Insert into store
	if err := rh.store.Insert(rh.tableName, item); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to insert: %w", err)))
		return
	}

	// Return created item
	rh.writeResponse(w, item)
}

// handleUpdate handles Update<Resource> RPC
func (rh *ResourceHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}

	// Get resource data from request
	resourceData, ok := req[rh.resource.Name]
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s is required", rh.resource.Name)))
		return
	}

	// Convert to map
	item, ok := resourceData.(map[string]any)
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid %s data", rh.resource.Name)))
		return
	}

	// Get ID
	id, ok := item["id"]
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required")))
		return
	}

	// Update in store
	if err := rh.store.Update(rh.tableName, fmt.Sprintf("%v", id), item); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeNotFound, err))
		return
	}

	// Return updated item
	rh.writeResponse(w, item)
}

// handleDelete handles Delete<Resource> RPC
func (rh *ResourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err)))
		return
	}

	// Get ID from request
	id, ok := req["id"]
	if !ok {
		rh.writeError(w, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required")))
		return
	}

	// Delete from store
	if err := rh.store.Delete(rh.tableName, fmt.Sprintf("%v", id)); err != nil {
		rh.writeError(w, connect.NewError(connect.CodeNotFound, err))
		return
	}

	// Return empty response
	rh.writeResponse(w, map[string]any{})
}

// writeResponse writes a successful Connect-RPC response
func (rh *ResourceHandler) writeResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	data, err := json.Marshal(resp)
	if err != nil {
		rh.writeError(w, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal response: %w", err)))
		return
	}

	w.Write(data)
}

// writeError writes a Connect-RPC error response
func (rh *ResourceHandler) writeError(w http.ResponseWriter, err *connect.Error) {
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

// mapFieldType maps fake data types to resource field types
func mapFieldType(fakeType string) resource.FieldType {
	switch fakeType {
	case "uuid":
		return resource.FieldTypeString
	case "name", "email":
		return resource.FieldTypeString
	case "int":
		return resource.FieldTypeInt
	case "decimal":
		return resource.FieldTypeFloat
	case "bool":
		return resource.FieldTypeBool
	case "date", "datetime":
		return resource.FieldTypeString
	case "enum", "ref":
		return resource.FieldTypeString
	default:
		return resource.FieldTypeString
	}
}

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
