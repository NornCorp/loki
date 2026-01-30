package fake

import (
	"fmt"

	"github.com/brianvoe/gofakeit/v6"
)

// Generator generates fake data based on field configurations
type Generator struct {
	faker *gofakeit.Faker
}

// NewGenerator creates a new fake data generator
func NewGenerator() *Generator {
	return &Generator{
		faker: gofakeit.New(0), // Random seed
	}
}

// NewSeededGenerator creates a new fake data generator with a specific seed
// This allows for reproducible data generation
func NewSeededGenerator(seed int64) *Generator {
	return &Generator{
		faker: gofakeit.New(seed),
	}
}

// Generate generates fake data for a single field
func (g *Generator) Generate(field FieldConfig) (any, error) {
	handler, ok := typeHandlers[field.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported fake type: %s", field.Type)
	}

	return handler(g.faker, field.Config)
}

// GenerateRow generates a complete row of fake data
func (g *Generator) GenerateRow(fields []FieldConfig) (map[string]any, error) {
	row := make(map[string]any)

	for _, field := range fields {
		value, err := g.Generate(field)
		if err != nil {
			return nil, fmt.Errorf("failed to generate field %s: %w", field.Name, err)
		}
		row[field.Name] = value
	}

	return row, nil
}

// GenerateRows generates multiple rows of fake data
func (g *Generator) GenerateRows(fields []FieldConfig, count int) ([]map[string]any, error) {
	if count < 0 {
		return nil, fmt.Errorf("count must be non-negative")
	}

	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		row, err := g.GenerateRow(fields)
		if err != nil {
			return nil, fmt.Errorf("failed to generate row %d: %w", i, err)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// SetSeed sets the random seed for reproducible generation
func (g *Generator) SetSeed(seed int64) {
	g.faker = gofakeit.New(seed)
}
