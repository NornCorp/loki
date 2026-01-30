package fake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewGenerator(t *testing.T) {
	gen := NewGenerator()
	require.NotNil(t, gen)
	require.NotNil(t, gen.faker)
}

func TestNewSeededGenerator(t *testing.T) {
	gen := NewSeededGenerator(12345)
	require.NotNil(t, gen)
	require.NotNil(t, gen.faker)
}

func TestGenerateUUID(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "id",
		Type: TypeUUID,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's a valid UUID string (36 characters with dashes)
	uuidStr, ok := value.(string)
	require.True(t, ok)
	require.Len(t, uuidStr, 36)
	require.Contains(t, uuidStr, "-")
}

func TestGenerateName(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "name",
		Type: TypeName,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's a non-empty string
	name, ok := value.(string)
	require.True(t, ok)
	require.NotEmpty(t, name)
}

func TestGenerateEmail(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "email",
		Type: TypeEmail,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's a valid email format
	email, ok := value.(string)
	require.True(t, ok)
	require.Contains(t, email, "@")
	require.Contains(t, email, ".")
}

func TestGenerateInt(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name:   "default range",
			config: nil,
		},
		{
			name: "custom range",
			config: map[string]any{
				"min": float64(10),
				"max": float64(20),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator()

			field := FieldConfig{
				Name:   "count",
				Type:   TypeInt,
				Config: tt.config,
			}

			value, err := gen.Generate(field)
			require.NoError(t, err)
			require.NotNil(t, value)

			num, ok := value.(int)
			require.True(t, ok)

			if tt.config != nil {
				min := int(tt.config["min"].(float64))
				max := int(tt.config["max"].(float64))
				require.GreaterOrEqual(t, num, min)
				require.LessOrEqual(t, num, max)
			}
		})
	}
}

func TestGenerateDecimal(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name:   "default range",
			config: nil,
		},
		{
			name: "custom range",
			config: map[string]any{
				"min": float64(1.5),
				"max": float64(10.5),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator()

			field := FieldConfig{
				Name:   "price",
				Type:   TypeDecimal,
				Config: tt.config,
			}

			value, err := gen.Generate(field)
			require.NoError(t, err)
			require.NotNil(t, value)

			num, ok := value.(float64)
			require.True(t, ok)

			if tt.config != nil {
				min := tt.config["min"].(float64)
				max := tt.config["max"].(float64)
				require.GreaterOrEqual(t, num, min)
				require.LessOrEqual(t, num, max)
			}
		})
	}
}

func TestGenerateBool(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "active",
		Type: TypeBool,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	_, ok := value.(bool)
	require.True(t, ok)
}

func TestGenerateDate(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "created_at",
		Type: TypeDate,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's a date string in YYYY-MM-DD format
	date, ok := value.(string)
	require.True(t, ok)
	require.Len(t, date, 10)
	require.Contains(t, date, "-")
}

func TestGenerateDateTime(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "created_at",
		Type: TypeDateTime,
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's a datetime string in RFC3339 format
	datetime, ok := value.(string)
	require.True(t, ok)
	require.Contains(t, datetime, "T")
	require.True(t, strings.HasSuffix(datetime, "Z"))
}

func TestGenerateEnum(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "status",
		Type: TypeEnum,
		Config: map[string]any{
			"values": []any{"active", "inactive", "pending"},
		},
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's one of the provided values
	status, ok := value.(string)
	require.True(t, ok)
	require.Contains(t, []string{"active", "inactive", "pending"}, status)
}

func TestGenerateEnumErrors(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name:   "missing config",
			config: nil,
		},
		{
			name:   "missing values",
			config: map[string]any{},
		},
		{
			name: "empty values",
			config: map[string]any{
				"values": []any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator()

			field := FieldConfig{
				Name:   "status",
				Type:   TypeEnum,
				Config: tt.config,
			}

			_, err := gen.Generate(field)
			require.Error(t, err)
		})
	}
}

func TestGenerateRef(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "user_id",
		Type: TypeRef,
		Config: map[string]any{
			"ids": []string{"id1", "id2", "id3"},
		},
	}

	value, err := gen.Generate(field)
	require.NoError(t, err)
	require.NotNil(t, value)

	// Verify it's one of the provided IDs
	id, ok := value.(string)
	require.True(t, ok)
	require.Contains(t, []string{"id1", "id2", "id3"}, id)
}

func TestGenerateRefErrors(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name:   "missing config",
			config: nil,
		},
		{
			name:   "missing ids",
			config: map[string]any{},
		},
		{
			name: "empty ids",
			config: map[string]any{
				"ids": []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator()

			field := FieldConfig{
				Name:   "user_id",
				Type:   TypeRef,
				Config: tt.config,
			}

			_, err := gen.Generate(field)
			require.Error(t, err)
		})
	}
}

func TestGenerateRow(t *testing.T) {
	gen := NewGenerator()

	fields := []FieldConfig{
		{Name: "id", Type: TypeUUID},
		{Name: "name", Type: TypeName},
		{Name: "email", Type: TypeEmail},
		{Name: "age", Type: TypeInt, Config: map[string]any{"min": float64(18), "max": float64(65)}},
		{Name: "active", Type: TypeBool},
	}

	row, err := gen.GenerateRow(fields)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Len(t, row, 5)

	// Verify all fields are present
	require.Contains(t, row, "id")
	require.Contains(t, row, "name")
	require.Contains(t, row, "email")
	require.Contains(t, row, "age")
	require.Contains(t, row, "active")

	// Verify types
	_, ok := row["id"].(string)
	require.True(t, ok)
	_, ok = row["name"].(string)
	require.True(t, ok)
	_, ok = row["email"].(string)
	require.True(t, ok)
	_, ok = row["age"].(int)
	require.True(t, ok)
	_, ok = row["active"].(bool)
	require.True(t, ok)
}

func TestGenerateRows(t *testing.T) {
	gen := NewGenerator()

	fields := []FieldConfig{
		{Name: "id", Type: TypeUUID},
		{Name: "name", Type: TypeName},
	}

	rows, err := gen.GenerateRows(fields, 10)
	require.NoError(t, err)
	require.NotNil(t, rows)
	require.Len(t, rows, 10)

	// Verify each row has the expected fields
	for _, row := range rows {
		require.Len(t, row, 2)
		require.Contains(t, row, "id")
		require.Contains(t, row, "name")
	}
}

func TestGenerateRowsZeroCount(t *testing.T) {
	gen := NewGenerator()

	fields := []FieldConfig{
		{Name: "id", Type: TypeUUID},
	}

	rows, err := gen.GenerateRows(fields, 0)
	require.NoError(t, err)
	require.NotNil(t, rows)
	require.Len(t, rows, 0)
}

func TestGenerateRowsNegativeCount(t *testing.T) {
	gen := NewGenerator()

	fields := []FieldConfig{
		{Name: "id", Type: TypeUUID},
	}

	_, err := gen.GenerateRows(fields, -1)
	require.Error(t, err)
}

func TestSeededGenerationIsReproducible(t *testing.T) {
	seed := int64(42)

	// Generate data with first generator
	gen1 := NewSeededGenerator(seed)
	fields := []FieldConfig{
		{Name: "id", Type: TypeUUID},
		{Name: "name", Type: TypeName},
		{Name: "email", Type: TypeEmail},
	}
	rows1, err := gen1.GenerateRows(fields, 5)
	require.NoError(t, err)

	// Generate data with second generator using same seed
	gen2 := NewSeededGenerator(seed)
	rows2, err := gen2.GenerateRows(fields, 5)
	require.NoError(t, err)

	// Verify the data is identical
	require.Equal(t, rows1, rows2)
}

func TestSetSeed(t *testing.T) {
	gen := NewGenerator()
	seed := int64(12345)

	fields := []FieldConfig{
		{Name: "name", Type: TypeName},
	}

	// Generate with one seed
	gen.SetSeed(seed)
	row1, err := gen.GenerateRow(fields)
	require.NoError(t, err)

	// Reset to same seed and generate again
	gen.SetSeed(seed)
	row2, err := gen.GenerateRow(fields)
	require.NoError(t, err)

	// Should be identical
	require.Equal(t, row1, row2)
}

func TestUnsupportedType(t *testing.T) {
	gen := NewGenerator()

	field := FieldConfig{
		Name: "test",
		Type: FakeType("unsupported"),
	}

	_, err := gen.Generate(field)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported fake type")
}
