package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gertd/go-pluralize"
	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/fake"
	"github.com/norncorp/loki/internal/resource"
)

// ResourceHandler handles auto-generated REST endpoints for a resource
type ResourceHandler struct {
	resource   *config.ResourceConfig
	store      *resource.Store
	pluralName string
	idPattern  *regexp.Regexp
}

// NewResourceHandler creates a new resource handler
func NewResourceHandler(res *config.ResourceConfig, store *resource.Store) (*ResourceHandler, error) {
	// Derive plural name
	pluralizer := pluralize.NewClient()
	pluralName := pluralizer.Plural(res.Name)

	// Compile ID pattern for matching /<plural>/:id routes
	pattern := fmt.Sprintf("^/%s/([^/]+)$", pluralName)
	idPattern, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile ID pattern: %w", err)
	}

	return &ResourceHandler{
		resource:   res,
		store:      store,
		pluralName: pluralName,
		idPattern:  idPattern,
	}, nil
}

// Initialize sets up the resource store and generates initial data
func (rh *ResourceHandler) Initialize() error {
	// Create table schema
	schema := resource.Schema{
		Name:   rh.resource.Name,
		Fields: make([]resource.Field, 0, len(rh.resource.Fields)),
	}

	for _, field := range rh.resource.Fields {
		resourceField := resource.Field{
			Name:  field.Name,
			Type:  rh.mapFieldType(field.Type),
			Index: false, // Could be enhanced to support indexing
		}

		// First field is typically the primary key
		if len(schema.Fields) == 0 {
			resourceField.PrimaryKey = true
			resourceField.Index = true
		}

		schema.Fields = append(schema.Fields, resourceField)
	}

	// Create table
	if err := rh.store.CreateTable(rh.resource.Name, schema); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Generate initial data
	if rh.resource.Rows > 0 {
		if err := rh.generateData(); err != nil {
			return fmt.Errorf("failed to generate data: %w", err)
		}
	}

	return nil
}

// mapFieldType converts config field type to resource field type
func (rh *ResourceHandler) mapFieldType(typ string) resource.FieldType {
	switch typ {
	case "uuid", "name", "email", "date", "datetime", "enum", "ref":
		return resource.FieldTypeString
	case "int":
		return resource.FieldTypeInt
	case "decimal":
		return resource.FieldTypeFloat
	case "bool":
		return resource.FieldTypeBool
	default:
		return resource.FieldTypeString
	}
}

// generateData generates fake data for the resource
func (rh *ResourceHandler) generateData() error {
	var gen *fake.Generator
	if rh.resource.Seed != nil {
		gen = fake.NewSeededGenerator(*rh.resource.Seed)
	} else {
		gen = fake.NewGenerator()
	}

	// Convert config fields to fake field configs
	fakeFields := make([]fake.FieldConfig, 0, len(rh.resource.Fields))
	for _, field := range rh.resource.Fields {
		fakeField := fake.FieldConfig{
			Name:   field.Name,
			Type:   fake.FakeType(field.Type),
			Config: field.Config,
		}

		// Handle min/max for numeric types
		if field.Min != nil || field.Max != nil {
			if fakeField.Config == nil {
				fakeField.Config = make(map[string]any)
			}
			if field.Min != nil {
				fakeField.Config["min"] = *field.Min
			}
			if field.Max != nil {
				fakeField.Config["max"] = *field.Max
			}
		}

		// Handle values for enum types
		if len(field.Values) > 0 {
			if fakeField.Config == nil {
				fakeField.Config = make(map[string]any)
			}
			values := make([]any, len(field.Values))
			for i, v := range field.Values {
				values[i] = v
			}
			fakeField.Config["values"] = values
		}

		fakeFields = append(fakeFields, fakeField)
	}

	// Generate rows
	rows, err := gen.GenerateRows(fakeFields, rh.resource.Rows)
	if err != nil {
		return fmt.Errorf("failed to generate rows: %w", err)
	}

	// Insert into store
	for _, row := range rows {
		if err := rh.store.Insert(rh.resource.Name, row); err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	return nil
}

// Match checks if the request matches this resource's routes
func (rh *ResourceHandler) Match(method, path string) bool {
	listPath := "/" + rh.pluralName

	switch method {
	case "GET":
		// GET /resources or GET /resources/:id
		return path == listPath || rh.idPattern.MatchString(path)
	case "POST":
		// POST /resources
		return path == listPath
	case "PUT":
		// PUT /resources/:id
		return rh.idPattern.MatchString(path)
	case "DELETE":
		// DELETE /resources/:id
		return rh.idPattern.MatchString(path)
	default:
		return false
	}
}

// extractID extracts the ID from a path like /resources/:id
func (rh *ResourceHandler) extractID(path string) (string, bool) {
	matches := rh.idPattern.FindStringSubmatch(path)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

// Handle handles a request to this resource
func (rh *ResourceHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		if strings.HasSuffix(r.URL.Path, "/"+rh.pluralName) {
			rh.handleList(w, r)
		} else {
			rh.handleGet(w, r)
		}
	case "POST":
		rh.handleCreate(w, r)
	case "PUT":
		rh.handleUpdate(w, r)
	case "DELETE":
		rh.handleDelete(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleList handles GET /resources
func (rh *ResourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	items, err := rh.store.List(rh.resource.Name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to list items: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// TODO: Add pagination support
	response := map[string]any{
		"data":  items,
		"total": len(items),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleGet handles GET /resources/:id
func (rh *ResourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := rh.extractID(r.URL.Path)
	if !ok {
		http.Error(w, `{"error":"invalid ID"}`, http.StatusBadRequest)
		return
	}

	item, err := rh.store.Get(rh.resource.Name, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf(`{"error":"failed to get item: %v"}`, err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(item)
}

// handleCreate handles POST /resources
func (rh *ResourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var item map[string]any
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	if err := rh.store.Insert(rh.resource.Name, item); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create item: %v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// handleUpdate handles PUT /resources/:id
func (rh *ResourceHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := rh.extractID(r.URL.Path)
	if !ok {
		http.Error(w, `{"error":"invalid ID"}`, http.StatusBadRequest)
		return
	}

	var item map[string]any
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	if err := rh.store.Update(rh.resource.Name, id, item); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf(`{"error":"failed to update item: %v"}`, err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(item)
}

// handleDelete handles DELETE /resources/:id
func (rh *ResourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := rh.extractID(r.URL.Path)
	if !ok {
		http.Error(w, `{"error":"invalid ID"}`, http.StatusBadRequest)
		return
	}

	if err := rh.store.Delete(rh.resource.Name, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf(`{"error":"failed to delete item: %v"}`, err), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
