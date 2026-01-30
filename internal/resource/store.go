package resource

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-memdb"
)

// Store provides an in-memory mutable state store for resources
type Store struct {
	db      *memdb.MemDB
	schemas map[string]*Schema
	mu      sync.RWMutex
}

// NewStore creates a new resource store
func NewStore() *Store {
	return &Store{
		schemas: make(map[string]*Schema),
	}
}

// CreateTable creates a new table with the given schema
func (s *Store) CreateTable(name string, schema Schema) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.schemas[name]; exists {
		return fmt.Errorf("table %s already exists", name)
	}

	// Convert to memdb schema
	tableSchema, err := schema.ToMemDBSchema()
	if err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	// Create or recreate database with new table
	dbSchema := &memdb.DBSchema{
		Tables: make(map[string]*memdb.TableSchema),
	}

	// Add existing tables
	for existingName, existingSchema := range s.schemas {
		ts, err := existingSchema.ToMemDBSchema()
		if err != nil {
			return fmt.Errorf("failed to convert existing schema %s: %w", existingName, err)
		}
		dbSchema.Tables[existingName] = ts
	}

	// Add new table
	dbSchema.Tables[name] = tableSchema

	// Create new database
	db, err := memdb.NewMemDB(dbSchema)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	// If we have an existing database, copy data
	if s.db != nil {
		if err := s.copyData(db); err != nil {
			return fmt.Errorf("failed to copy existing data: %w", err)
		}
	}

	s.db = db
	s.schemas[name] = &schema

	return nil
}

// copyData copies all data from the current database to the new database
func (s *Store) copyData(newDB *memdb.MemDB) error {
	txn := s.db.Txn(false)
	defer txn.Abort()

	newTxn := newDB.Txn(true)
	defer newTxn.Abort()

	for tableName := range s.schemas {
		it, err := txn.Get(tableName, "id")
		if err != nil {
			return fmt.Errorf("failed to read table %s: %w", tableName, err)
		}

		for obj := it.Next(); obj != nil; obj = it.Next() {
			if err := newTxn.Insert(tableName, obj); err != nil {
				return fmt.Errorf("failed to insert into table %s: %w", tableName, err)
			}
		}
	}

	newTxn.Commit()
	return nil
}

// Insert adds a new item to the table
func (s *Store) Insert(table string, item map[string]any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return fmt.Errorf("no tables created")
	}

	schema, exists := s.schemas[table]
	if !exists {
		return fmt.Errorf("table %s does not exist", table)
	}

	// Validate item has required fields
	var pkField *Field
	for i := range schema.Fields {
		if schema.Fields[i].PrimaryKey {
			pkField = &schema.Fields[i]
			break
		}
	}

	if pkField == nil {
		return fmt.Errorf("schema has no primary key")
	}

	if _, ok := item[pkField.Name]; !ok {
		return fmt.Errorf("item missing primary key field: %s", pkField.Name)
	}

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert(table, item); err != nil {
		return fmt.Errorf("failed to insert item: %w", err)
	}

	txn.Commit()
	return nil
}

// Get retrieves a single item by its ID
func (s *Store) Get(table, id string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil, fmt.Errorf("no tables created")
	}

	if _, exists := s.schemas[table]; !exists {
		return nil, fmt.Errorf("table %s does not exist", table)
	}

	txn := s.db.Txn(false)
	defer txn.Abort()

	obj, err := txn.First(table, "id", id)
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	if obj == nil {
		return nil, fmt.Errorf("item not found")
	}

	item, ok := obj.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid item type")
	}

	return item, nil
}

// List retrieves all items from a table
func (s *Store) List(table string) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil, fmt.Errorf("no tables created")
	}

	if _, exists := s.schemas[table]; !exists {
		return nil, fmt.Errorf("table %s does not exist", table)
	}

	txn := s.db.Txn(false)
	defer txn.Abort()

	it, err := txn.Get(table, "id")
	if err != nil {
		return nil, fmt.Errorf("failed to list items: %w", err)
	}

	var items []map[string]any
	for obj := it.Next(); obj != nil; obj = it.Next() {
		item, ok := obj.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid item type")
		}
		items = append(items, item)
	}

	return items, nil
}

// Where retrieves items matching a field value
func (s *Store) Where(table, field string, value any) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil, fmt.Errorf("no tables created")
	}

	schema, exists := s.schemas[table]
	if !exists {
		return nil, fmt.Errorf("table %s does not exist", table)
	}

	// Check if field is indexed
	var fieldSchema *Field
	for i := range schema.Fields {
		if schema.Fields[i].Name == field {
			fieldSchema = &schema.Fields[i]
			break
		}
	}

	if fieldSchema == nil {
		return nil, fmt.Errorf("field %s does not exist in table %s", field, table)
	}

	txn := s.db.Txn(false)
	defer txn.Abort()

	var it memdb.ResultIterator
	var err error

	// If field is indexed, use index
	if fieldSchema.Index || fieldSchema.PrimaryKey {
		indexName := field
		if fieldSchema.PrimaryKey {
			indexName = "id"
		}
		it, err = txn.Get(table, indexName, value)
		if err != nil {
			return nil, fmt.Errorf("failed to query by index: %w", err)
		}
	} else {
		// Otherwise, scan all and filter
		it, err = txn.Get(table, "id")
		if err != nil {
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}
	}

	var items []map[string]any
	for obj := it.Next(); obj != nil; obj = it.Next() {
		item, ok := obj.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid item type")
		}

		// If not using index, filter manually
		if !fieldSchema.Index && !fieldSchema.PrimaryKey {
			if item[field] != value {
				continue
			}
		}

		items = append(items, item)
	}

	return items, nil
}

// Update modifies an existing item
func (s *Store) Update(table, id string, item map[string]any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return fmt.Errorf("no tables created")
	}

	schema, exists := s.schemas[table]
	if !exists {
		return fmt.Errorf("table %s does not exist", table)
	}

	// Find primary key field
	var pkField *Field
	for i := range schema.Fields {
		if schema.Fields[i].PrimaryKey {
			pkField = &schema.Fields[i]
			break
		}
	}

	if pkField == nil {
		return fmt.Errorf("schema has no primary key")
	}

	// Ensure item has the correct ID
	item[pkField.Name] = id

	txn := s.db.Txn(true)
	defer txn.Abort()

	// Check if item exists
	existing, err := txn.First(table, "id", id)
	if err != nil {
		return fmt.Errorf("failed to check for existing item: %w", err)
	}

	if existing == nil {
		return fmt.Errorf("item not found")
	}

	// Delete old version
	if err := txn.Delete(table, existing); err != nil {
		return fmt.Errorf("failed to delete old item: %w", err)
	}

	// Insert new version
	if err := txn.Insert(table, item); err != nil {
		return fmt.Errorf("failed to insert updated item: %w", err)
	}

	txn.Commit()
	return nil
}

// Delete removes an item from the table
func (s *Store) Delete(table, id string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return fmt.Errorf("no tables created")
	}

	if _, exists := s.schemas[table]; !exists {
		return fmt.Errorf("table %s does not exist", table)
	}

	txn := s.db.Txn(true)
	defer txn.Abort()

	// Get the item
	obj, err := txn.First(table, "id", id)
	if err != nil {
		return fmt.Errorf("failed to get item: %w", err)
	}

	if obj == nil {
		return fmt.Errorf("item not found")
	}

	// Delete it
	if err := txn.Delete(table, obj); err != nil {
		return fmt.Errorf("failed to delete item: %w", err)
	}

	txn.Commit()
	return nil
}
