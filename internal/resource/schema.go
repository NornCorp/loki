package resource

import (
	"fmt"

	"github.com/hashicorp/go-memdb"
)

// Schema defines the structure of a resource table
type Schema struct {
	Name   string
	Fields []Field
}

// Field defines a single field in a resource schema
type Field struct {
	Name       string
	Type       FieldType
	PrimaryKey bool
	Index      bool
}

// FieldType represents the data type of a field
type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeInt     FieldType = "int"
	FieldTypeBool    FieldType = "bool"
	FieldTypeFloat   FieldType = "float"
	FieldTypeAny     FieldType = "any"
)

// ToMemDBSchema converts a Schema to a go-memdb TableSchema
func (s *Schema) ToMemDBSchema() (*memdb.TableSchema, error) {
	if s.Name == "" {
		return nil, fmt.Errorf("schema name is required")
	}

	if len(s.Fields) == 0 {
		return nil, fmt.Errorf("schema must have at least one field")
	}

	// Find primary key field
	var pkField *Field
	for i := range s.Fields {
		if s.Fields[i].PrimaryKey {
			if pkField != nil {
				return nil, fmt.Errorf("multiple primary key fields found")
			}
			pkField = &s.Fields[i]
		}
	}

	if pkField == nil {
		return nil, fmt.Errorf("schema must have a primary key field")
	}

	// Build indexes
	indexes := make(map[string]*memdb.IndexSchema)

	// Add primary key index
	indexes["id"] = &memdb.IndexSchema{
		Name:    "id",
		Unique:  true,
		Indexer: &MapFieldIndexer{Field: pkField.Name},
	}

	// Add additional indexes
	for i := range s.Fields {
		field := &s.Fields[i]
		if field.Index && !field.PrimaryKey {
			indexer, err := s.createIndexer(field)
			if err != nil {
				return nil, fmt.Errorf("failed to create indexer for field %s: %w", field.Name, err)
			}

			indexes[field.Name] = &memdb.IndexSchema{
				Name:    field.Name,
				Unique:  false,
				Indexer: indexer,
			}
		}
	}

	return &memdb.TableSchema{
		Name:    s.Name,
		Indexes: indexes,
	}, nil
}

// createIndexer creates an appropriate indexer for the field type
func (s *Schema) createIndexer(field *Field) (memdb.Indexer, error) {
	// Use custom map indexer for all field types
	return &MapFieldIndexer{Field: field.Name}, nil
}
