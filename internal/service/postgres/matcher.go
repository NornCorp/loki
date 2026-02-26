package postgres

import (
	"fmt"
	"strings"

	"github.com/gertd/go-pluralize"
	"github.com/norncorp/loki/internal/resource"
)

// TableColumn describes a column registered with the query matcher.
type TableColumn struct {
	Name    string
	Type    string
	TypeOID int32
}

// QueryResult holds the result of executing a query.
type QueryResult struct {
	Columns []ColumnDef
	Rows    [][]string
	Tag     string
}

type customPattern struct {
	pattern   string
	fromTable string
	where     string
}

// QueryMatcher matches SQL queries to table data.
type QueryMatcher struct {
	store     *resource.Store
	tables    map[string][]TableColumn // table name -> columns
	patterns  []customPattern
	pluralizer *pluralize.Client
}

// NewQueryMatcher creates a new query matcher backed by the given store.
func NewQueryMatcher(store *resource.Store) *QueryMatcher {
	return &QueryMatcher{
		store:      store,
		tables:     make(map[string][]TableColumn),
		pluralizer: pluralize.NewClient(),
	}
}

// RegisterTable registers a table and its columns with the matcher.
// Both singular and plural forms are registered for lookup.
func (m *QueryMatcher) RegisterTable(name string, columns []TableColumn) {
	m.tables[name] = columns
	plural := m.pluralizer.Plural(name)
	if plural != name {
		m.tables[plural] = columns
	}
}

// AddPattern adds a custom query pattern.
func (m *QueryMatcher) AddPattern(pattern, fromTable, where string) {
	m.patterns = append(m.patterns, customPattern{
		pattern:   normalizeSQL(pattern),
		fromTable: fromTable,
		where:     where,
	})
}

// Execute matches and executes a SQL query, returning the result.
func (m *QueryMatcher) Execute(query string) (*QueryResult, error) {
	normalized := normalizeSQL(query)
	// Preserve original casing for value extraction (same whitespace normalization, no lowercasing)
	preserved := normalizeWhitespace(query)

	// Try custom patterns first (use preserved query for case-sensitive captures)
	for _, p := range m.patterns {
		if captures := matchPattern(p.pattern, normalized); captures != nil {
			// Re-extract captures from preserved query to maintain original case
			preservedCaptures := matchPatternCaseInsensitive(p.pattern, preserved)
			if preservedCaptures != nil {
				return m.executeCustom(p, preservedCaptures)
			}
			return m.executeCustom(p, captures)
		}
	}

	return m.executeAuto(normalized, preserved)
}

func (m *QueryMatcher) executeAuto(normalized, preserved string) (*QueryResult, error) {
	words := strings.Fields(normalized)
	if len(words) == 0 {
		return &QueryResult{Tag: "EMPTY"}, nil
	}

	switch words[0] {
	case "select":
		return m.handleSelect(normalized)
	case "insert":
		return m.handleInsert(normalized, preserved)
	case "update":
		return m.handleUpdate(normalized, preserved)
	case "delete":
		return m.handleDelete(normalized)
	case "set":
		return &QueryResult{Tag: "SET"}, nil
	case "show":
		return m.handleShow(normalized)
	case "begin":
		return &QueryResult{Tag: "BEGIN"}, nil
	case "commit":
		return &QueryResult{Tag: "COMMIT"}, nil
	case "rollback":
		return &QueryResult{Tag: "ROLLBACK"}, nil
	case "discard":
		return &QueryResult{Tag: "DISCARD ALL"}, nil
	case "reset":
		return &QueryResult{Tag: "RESET"}, nil
	case "close":
		return &QueryResult{Tag: "CLOSE CURSOR"}, nil
	case "deallocate":
		return &QueryResult{Tag: "DEALLOCATE"}, nil
	default:
		return nil, fmt.Errorf("unsupported query: %s", normalized)
	}
}

func (m *QueryMatcher) resolveTable(name string) (string, []TableColumn, error) {
	if cols, ok := m.tables[name]; ok {
		storeTable := m.pluralizer.Singular(name)
		return storeTable, cols, nil
	}
	singular := m.pluralizer.Singular(name)
	if cols, ok := m.tables[singular]; ok {
		return singular, cols, nil
	}
	return "", nil, fmt.Errorf("table %q does not exist", name)
}

func (m *QueryMatcher) handleSelectExpr(normalized string) (*QueryResult, error) {
	// Handle common function calls and expressions
	if strings.Contains(normalized, "version()") {
		return &QueryResult{
			Columns: []ColumnDef{{Name: "version", TypeOID: oidText}},
			Rows:    [][]string{{"PostgreSQL 16.0 (Loki fake database)"}},
			Tag:     "SELECT 1",
		}, nil
	}
	if strings.Contains(normalized, "current_database()") {
		return &QueryResult{
			Columns: []ColumnDef{{Name: "current_database", TypeOID: oidText}},
			Rows:    [][]string{{"myapp"}},
			Tag:     "SELECT 1",
		}, nil
	}
	if strings.Contains(normalized, "current_user") || strings.Contains(normalized, "current_schema") {
		return &QueryResult{
			Columns: []ColumnDef{{Name: "current_user", TypeOID: oidText}},
			Rows:    [][]string{{"app"}},
			Tag:     "SELECT 1",
		}, nil
	}
	// Generic: return the expression text as a single row
	expr := strings.TrimPrefix(normalized, "select ")
	return &QueryResult{
		Columns: []ColumnDef{{Name: "?column?", TypeOID: oidText}},
		Rows:    [][]string{{expr}},
		Tag:     "SELECT 1",
	}, nil
}

func (m *QueryMatcher) handleShow(normalized string) (*QueryResult, error) {
	words := strings.Fields(normalized)
	if len(words) < 2 {
		return &QueryResult{Tag: "SHOW"}, nil
	}
	param := words[1]
	value := "on"
	switch param {
	case "transaction_isolation", "default_transaction_isolation":
		value = "read committed"
	case "server_version":
		value = "16.0 (Loki)"
	case "server_encoding":
		value = "UTF8"
	case "client_encoding":
		value = "UTF8"
	case "standard_conforming_strings":
		value = "on"
	case "datestyle":
		value = "ISO, MDY"
	}
	return &QueryResult{
		Columns: []ColumnDef{{Name: param, TypeOID: oidText}},
		Rows:    [][]string{{value}},
		Tag:     "SHOW",
	}, nil
}

func (m *QueryMatcher) handleSelect(normalized string) (*QueryResult, error) {
	// Handle SELECT without FROM (function calls, constants)
	if !strings.Contains(normalized, " from ") {
		return m.handleSelectExpr(normalized)
	}

	tableName := extractTableName(normalized, "from")
	if tableName == "" {
		return &QueryResult{Tag: "SELECT 0"}, nil
	}

	// Return empty results for system catalog queries
	if strings.HasPrefix(tableName, "pg_") || strings.HasPrefix(tableName, "information_schema") {
		return &QueryResult{Tag: "SELECT 0"}, nil
	}

	storeTable, cols, err := m.resolveTable(tableName)
	if err != nil {
		return nil, err
	}

	field, value := extractWhereEquals(normalized)

	var items []map[string]any
	if field != "" && value != "" {
		if field == "id" {
			item, err := m.store.Get(storeTable, value)
			if err != nil {
				return nil, err
			}
			if item != nil {
				items = []map[string]any{item}
			}
		} else {
			items, err = m.store.Where(storeTable, field, value)
			if err != nil {
				return nil, err
			}
		}
	} else {
		items, err = m.store.List(storeTable)
		if err != nil {
			return nil, err
		}
	}

	// Apply LIMIT
	if limit := extractLimit(normalized); limit >= 0 && limit < len(items) {
		items = items[:limit]
	}

	return m.buildSelectResult(cols, items), nil
}

func (m *QueryMatcher) handleInsert(normalized, preserved string) (*QueryResult, error) {
	tableName := extractTableName(normalized, "into")
	if tableName == "" {
		return nil, fmt.Errorf("cannot determine table name from INSERT")
	}

	storeTable, _, err := m.resolveTable(tableName)
	if err != nil {
		return nil, err
	}

	columns := extractParenList(normalized, "into "+tableName)
	// Use preserved (case-sensitive) query for value extraction
	values := extractParenListCaseInsensitive(preserved, "values")

	if len(columns) == 0 || len(values) == 0 || len(columns) != len(values) {
		return nil, fmt.Errorf("invalid INSERT syntax")
	}

	row := make(map[string]any)
	for i, col := range columns {
		row[col] = values[i]
	}

	if err := m.store.Insert(storeTable, row); err != nil {
		return nil, err
	}

	return &QueryResult{Tag: "INSERT 0 1"}, nil
}

func (m *QueryMatcher) handleUpdate(normalized, preserved string) (*QueryResult, error) {
	tableName := extractTableName(normalized, "update")
	if tableName == "" {
		return nil, fmt.Errorf("cannot determine table name from UPDATE")
	}

	storeTable, _, err := m.resolveTable(tableName)
	if err != nil {
		return nil, err
	}

	// Use preserved (case-sensitive) query for value extraction
	setAssigns := extractSetAssignments(preserved)
	if len(setAssigns) == 0 {
		return nil, fmt.Errorf("no SET assignments in UPDATE")
	}

	field, value := extractWhereEquals(preserved)
	if field == "" || value == "" {
		return nil, fmt.Errorf("UPDATE requires WHERE clause")
	}

	var items []map[string]any
	if field == "id" {
		item, getErr := m.store.Get(storeTable, value)
		if getErr != nil {
			return nil, getErr
		}
		if item != nil {
			items = []map[string]any{item}
		}
	} else {
		items, err = m.store.Where(storeTable, field, value)
		if err != nil {
			return nil, err
		}
	}

	count := 0
	for _, item := range items {
		for k, v := range setAssigns {
			item[k] = v
		}
		id, _ := item["id"].(string)
		if err := m.store.Update(storeTable, id, item); err != nil {
			return nil, err
		}
		count++
	}

	return &QueryResult{Tag: fmt.Sprintf("UPDATE %d", count)}, nil
}

func (m *QueryMatcher) handleDelete(normalized string) (*QueryResult, error) {
	tableName := extractTableName(normalized, "from")
	if tableName == "" {
		return nil, fmt.Errorf("cannot determine table name from DELETE")
	}

	storeTable, _, err := m.resolveTable(tableName)
	if err != nil {
		return nil, err
	}

	field, value := extractWhereEquals(normalized)
	if field == "" || value == "" {
		return nil, fmt.Errorf("DELETE requires WHERE clause")
	}

	var count int
	if field == "id" {
		if err := m.store.Delete(storeTable, value); err != nil {
			return nil, err
		}
		count = 1
	} else {
		items, err := m.store.Where(storeTable, field, value)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			id, _ := item["id"].(string)
			if err := m.store.Delete(storeTable, id); err != nil {
				return nil, err
			}
			count++
		}
	}

	return &QueryResult{Tag: fmt.Sprintf("DELETE %d", count)}, nil
}

func (m *QueryMatcher) executeCustom(p customPattern, captures []string) (*QueryResult, error) {
	if p.fromTable == "" {
		return &QueryResult{Tag: "SELECT 0"}, nil
	}

	storeTable, cols, err := m.resolveTable(p.fromTable)
	if err != nil {
		return nil, err
	}

	var items []map[string]any

	if p.where != "" {
		// Substitute captures into where clause
		where := p.where
		for i, cap := range captures {
			where = strings.ReplaceAll(where, fmt.Sprintf("${%d}", i+1), cap)
		}
		parts := strings.SplitN(where, "=", 2)
		if len(parts) == 2 {
			field := strings.TrimSpace(parts[0])
			value := unquoteValue(strings.TrimSpace(parts[1]))
			items, err = m.store.Where(storeTable, field, value)
		} else {
			items, err = m.store.List(storeTable)
		}
	} else {
		items, err = m.store.List(storeTable)
	}
	if err != nil {
		return nil, err
	}

	return m.buildSelectResult(cols, items), nil
}

func (m *QueryMatcher) buildSelectResult(cols []TableColumn, items []map[string]any) *QueryResult {
	colDefs := make([]ColumnDef, len(cols))
	for i, c := range cols {
		colDefs[i] = ColumnDef{Name: c.Name, TypeOID: c.TypeOID}
	}

	rows := make([][]string, len(items))
	for i, item := range items {
		row := make([]string, len(cols))
		for j, c := range cols {
			row[j] = fmt.Sprintf("%v", item[c.Name])
		}
		rows[i] = row
	}

	return &QueryResult{
		Columns: colDefs,
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(items)),
	}
}

// normalizeSQL normalizes a SQL query for matching (lowercased).
func normalizeSQL(sql string) string {
	return strings.ToLower(normalizeWhitespace(sql))
}

// normalizeWhitespace normalizes whitespace and trims semicolons but preserves case.
func normalizeWhitespace(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimRight(sql, ";")
	sql = strings.TrimSpace(sql)
	fields := strings.Fields(sql)
	return strings.Join(fields, " ")
}

// extractParenListCaseInsensitive finds keyword case-insensitively and
// extracts the parenthesized list that follows it, preserving original case.
func extractParenListCaseInsensitive(query, keyword string) []string {
	lower := strings.ToLower(query)
	kwLower := strings.ToLower(keyword)
	idx := strings.Index(lower, kwLower)
	if idx < 0 {
		return nil
	}
	rest := query[idx+len(keyword):]
	openIdx := strings.Index(rest, "(")
	if openIdx < 0 {
		return nil
	}
	closeIdx := strings.Index(rest[openIdx:], ")")
	if closeIdx < 0 {
		return nil
	}
	inner := rest[openIdx+1 : openIdx+closeIdx]
	parts := strings.Split(inner, ",")
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = unquoteValue(strings.TrimSpace(p))
	}
	return result
}

// matchPattern matches a pattern with * wildcards against a query.
// Returns captured wildcard values or nil if no match.
func matchPattern(pattern, query string) []string {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		if pattern == query {
			return []string{}
		}
		return nil
	}

	var captures []string
	remaining := query

	for i, part := range parts {
		if part == "" {
			if i == 0 {
				continue
			}
			if i == len(parts)-1 {
				captures = append(captures, remaining)
				return captures
			}
			continue
		}

		idx := strings.Index(remaining, part)
		if idx < 0 {
			return nil
		}
		if i > 0 {
			captures = append(captures, strings.TrimSpace(remaining[:idx]))
		} else if idx != 0 {
			return nil
		}
		remaining = remaining[idx+len(part):]
	}

	if !strings.HasSuffix(pattern, "*") && remaining != "" {
		return nil
	}
	if strings.HasSuffix(pattern, "*") && remaining != "" {
		captures = append(captures, strings.TrimSpace(remaining))
	}

	return captures
}

// matchPatternCaseInsensitive matches a lowercase pattern against a case-preserved
// query, extracting captures with original casing.
func matchPatternCaseInsensitive(pattern, query string) []string {
	lower := strings.ToLower(query)
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		if pattern == lower {
			return []string{}
		}
		return nil
	}

	var captures []string
	pos := 0

	for i, part := range parts {
		if part == "" {
			if i == 0 {
				continue
			}
			if i == len(parts)-1 {
				captures = append(captures, strings.TrimSpace(query[pos:]))
				return captures
			}
			continue
		}

		idx := strings.Index(lower[pos:], part)
		if idx < 0 {
			return nil
		}

		actualIdx := pos + idx
		if i > 0 {
			captures = append(captures, strings.TrimSpace(query[pos:actualIdx]))
		} else if actualIdx != 0 {
			return nil
		}
		pos = actualIdx + len(part)
	}

	if !strings.HasSuffix(pattern, "*") && pos < len(query) {
		return nil
	}
	if strings.HasSuffix(pattern, "*") && pos < len(query) {
		captures = append(captures, strings.TrimSpace(query[pos:]))
	}

	return captures
}

func extractTableName(normalized string, keyword string) string {
	idx := strings.Index(normalized, keyword+" ")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(normalized[idx+len(keyword)+1:])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func extractWhereEquals(query string) (string, string) {
	lower := strings.ToLower(query)
	idx := strings.Index(lower, "where ")
	if idx < 0 {
		return "", ""
	}
	clause := strings.TrimSpace(query[idx+6:])
	parts := strings.SplitN(clause, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	field := strings.ToLower(strings.TrimSpace(parts[0]))
	value := unquoteValue(strings.TrimSpace(parts[1]))
	return field, value
}

func extractParenList(normalized, after string) []string {
	idx := strings.Index(normalized, after)
	if idx < 0 {
		return nil
	}
	rest := normalized[idx+len(after):]
	openIdx := strings.Index(rest, "(")
	if openIdx < 0 {
		return nil
	}
	closeIdx := strings.Index(rest[openIdx:], ")")
	if closeIdx < 0 {
		return nil
	}
	inner := rest[openIdx+1 : openIdx+closeIdx]
	parts := strings.Split(inner, ",")
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = unquoteValue(strings.TrimSpace(p))
	}
	return result
}

func extractSetAssignments(query string) map[string]string {
	lower := strings.ToLower(query)
	idx := strings.Index(lower, "set ")
	if idx < 0 {
		return nil
	}
	rest := query[idx+4:]
	lowerRest := strings.ToLower(rest)
	if whereIdx := strings.Index(lowerRest, " where "); whereIdx >= 0 {
		rest = rest[:whereIdx]
	}
	assigns := make(map[string]string)
	pairs := strings.Split(rest, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := unquoteValue(strings.TrimSpace(parts[1]))
		assigns[key] = value
	}
	return assigns
}

func extractLimit(normalized string) int {
	idx := strings.Index(normalized, "limit ")
	if idx < 0 {
		return -1
	}
	rest := strings.TrimSpace(normalized[idx+6:])
	words := strings.Fields(rest)
	if len(words) == 0 {
		return -1
	}
	n := 0
	for _, ch := range words[0] {
		if ch < '0' || ch > '9' {
			return -1
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func unquoteValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}
