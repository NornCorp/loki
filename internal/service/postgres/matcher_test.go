package postgres

import (
	"testing"

	"github.com/jumppad-labs/polymorph/internal/resource"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SELECT * FROM users", "select * from users"},
		{"  SELECT  *  FROM  users  ;  ", "select * from users"},
		{"SELECT * FROM users;", "select * from users"},
		{"select\t*\nfrom\tusers", "select * from users"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.expected, normalizeSQL(tt.input))
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		query    string
		expected []string
	}{
		{
			name:     "exact match",
			pattern:  "select 1",
			query:    "select 1",
			expected: []string{},
		},
		{
			name:    "no match",
			pattern: "select 1",
			query:   "select 2",
		},
		{
			name:     "trailing wildcard",
			pattern:  "select * from users where status = *",
			query:    "select * from users where status = 'active'",
			expected: []string{"*", "'active'"},
		},
		{
			name:     "middle wildcard",
			pattern:  "select * from * where id = *",
			query:    "select * from users where id = '123'",
			expected: []string{"*", "users", "'123'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.query)
			if tt.expected == nil {
				require.Nil(t, result)
			} else {
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestExtractTableName(t *testing.T) {
	tests := []struct {
		query    string
		keyword  string
		expected string
	}{
		{"select * from users", "from", "users"},
		{"select * from users where id = '1'", "from", "users"},
		{"insert into users (id) values ('1')", "into", "users"},
		{"delete from orders where id = '1'", "from", "orders"},
		{"update users set name = 'x'", "update", "users"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			require.Equal(t, tt.expected, extractTableName(tt.query, tt.keyword))
		})
	}
}

func TestExtractWhereEquals(t *testing.T) {
	tests := []struct {
		query string
		field string
		value string
	}{
		{"select * from users where id = '123'", "id", "123"},
		{"select * from users where name = 'john'", "name", "john"},
		{"select * from users", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			f, v := extractWhereEquals(tt.query)
			require.Equal(t, tt.field, f)
			require.Equal(t, tt.value, v)
		})
	}
}

func TestUnquoteValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"'hello'", "hello"},
		{"hello", "hello"},
		{"''", ""},
		{"'it''s'", "it''s"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.expected, unquoteValue(tt.input))
		})
	}
}

func setupTestMatcher(t *testing.T) *QueryMatcher {
	t.Helper()

	store := resource.NewStore()
	err := store.CreateTable("user", resource.Schema{
		Name: "user",
		Fields: []resource.Field{
			{Name: "id", Type: resource.FieldTypeAny, PrimaryKey: true, Index: true},
			{Name: "name", Type: resource.FieldTypeAny},
			{Name: "email", Type: resource.FieldTypeAny},
		},
	})
	require.NoError(t, err)

	require.NoError(t, store.Insert("user", map[string]any{
		"id": "1", "name": "Alice", "email": "alice@test.com",
	}))
	require.NoError(t, store.Insert("user", map[string]any{
		"id": "2", "name": "Bob", "email": "bob@test.com",
	}))

	matcher := NewQueryMatcher(store)
	matcher.RegisterTable("user", []TableColumn{
		{Name: "id", Type: "uuid", TypeOID: oidUUID},
		{Name: "name", Type: "name", TypeOID: oidText},
		{Name: "email", Type: "email", TypeOID: oidText},
	})

	return matcher
}

func TestQueryMatcher_Select_ListAll(t *testing.T) {
	m := setupTestMatcher(t)

	result, err := m.Execute("SELECT * FROM users")
	require.NoError(t, err)
	require.Equal(t, "SELECT 2", result.Tag)
	require.Len(t, result.Rows, 2)
	require.Len(t, result.Columns, 3)
}

func TestQueryMatcher_Select_WhereID(t *testing.T) {
	m := setupTestMatcher(t)

	result, err := m.Execute("SELECT * FROM users WHERE id = '1'")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", result.Tag)
	require.Len(t, result.Rows, 1)
	require.Equal(t, "Alice", result.Rows[0][1])
}

func TestQueryMatcher_Select_PluralTable(t *testing.T) {
	m := setupTestMatcher(t)

	// "users" should resolve to "user" store table
	result, err := m.Execute("SELECT * FROM users")
	require.NoError(t, err)
	require.Equal(t, "SELECT 2", result.Tag)
}

func TestQueryMatcher_Insert(t *testing.T) {
	m := setupTestMatcher(t)

	result, err := m.Execute("INSERT INTO users (id, name, email) VALUES ('3', 'Charlie', 'charlie@test.com')")
	require.NoError(t, err)
	require.Equal(t, "INSERT 0 1", result.Tag)

	// Verify the insert
	selectResult, err := m.Execute("SELECT * FROM users WHERE id = '3'")
	require.NoError(t, err)
	require.Len(t, selectResult.Rows, 1)
	require.Equal(t, "Charlie", selectResult.Rows[0][1])
}

func TestQueryMatcher_Update(t *testing.T) {
	m := setupTestMatcher(t)

	result, err := m.Execute("UPDATE users SET name = 'Alice Smith' WHERE id = '1'")
	require.NoError(t, err)
	require.Equal(t, "UPDATE 1", result.Tag)

	// Verify the update
	selectResult, err := m.Execute("SELECT * FROM users WHERE id = '1'")
	require.NoError(t, err)
	require.Len(t, selectResult.Rows, 1)
	require.Equal(t, "Alice Smith", selectResult.Rows[0][1])
}

func TestQueryMatcher_Delete(t *testing.T) {
	m := setupTestMatcher(t)

	result, err := m.Execute("DELETE FROM users WHERE id = '2'")
	require.NoError(t, err)
	require.Equal(t, "DELETE 1", result.Tag)

	// Verify the delete
	selectResult, err := m.Execute("SELECT * FROM users")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", selectResult.Tag)
}

func TestQueryMatcher_CustomPattern(t *testing.T) {
	m := setupTestMatcher(t)
	// ${1} = column wildcard (*), ${2} = the actual name value
	m.AddPattern("select * from users where name = *", "user", "name = ${2}")

	result, err := m.Execute("SELECT * FROM users WHERE name = 'Alice'")
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.Equal(t, "Alice", result.Rows[0][1])
}

func TestQueryMatcher_UnknownTable(t *testing.T) {
	m := setupTestMatcher(t)

	_, err := m.Execute("SELECT * FROM nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestQueryMatcher_UnsupportedQuery(t *testing.T) {
	m := setupTestMatcher(t)

	_, err := m.Execute("DROP TABLE users")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}
