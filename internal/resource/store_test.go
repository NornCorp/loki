package resource

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewStore(t *testing.T) {
	store := NewStore()
	require.NotNil(t, store)
	require.NotNil(t, store.schemas)
	require.Nil(t, store.db)
}

func TestCreateTable(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
			{Name: "email", Type: FieldTypeString, Index: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)
	require.NotNil(t, store.db)
	require.Contains(t, store.schemas, "users")
}

func TestCreateTableDuplicate(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	err = store.CreateTable("users", schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestCreateTableNoPrimaryKey(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "primary key")
}

func TestInsert(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"id":   "user-1",
		"name": "Alice",
	}

	err = store.Insert("users", item)
	require.NoError(t, err)
}

func TestInsertMissingPrimaryKey(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"name": "Alice",
	}

	err = store.Insert("users", item)
	require.Error(t, err)
	require.Contains(t, err.Error(), "primary key")
}

func TestInsertNonexistentTable(t *testing.T) {
	store := NewStore()

	// Create a table first
	schema := Schema{
		Name: "other",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}
	err := store.CreateTable("other", schema)
	require.NoError(t, err)

	// Try to insert into nonexistent table
	item := map[string]any{
		"id": "user-1",
	}

	err = store.Insert("users", item)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestGet(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"id":   "user-1",
		"name": "Alice",
	}

	err = store.Insert("users", item)
	require.NoError(t, err)

	retrieved, err := store.Get("users", "user-1")
	require.NoError(t, err)
	require.Equal(t, "user-1", retrieved["id"])
	require.Equal(t, "Alice", retrieved["name"])
}

func TestGetNotFound(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	_, err = store.Get("users", "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestList(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	items := []map[string]any{
		{"id": "user-1", "name": "Alice"},
		{"id": "user-2", "name": "Bob"},
		{"id": "user-3", "name": "Charlie"},
	}

	for _, item := range items {
		err = store.Insert("users", item)
		require.NoError(t, err)
	}

	list, err := store.List("users")
	require.NoError(t, err)
	require.Len(t, list, 3)
}

func TestListEmpty(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	list, err := store.List("users")
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestWhereIndexedField(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "email", Type: FieldTypeString, Index: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	items := []map[string]any{
		{"id": "user-1", "name": "Alice", "email": "alice@example.com"},
		{"id": "user-2", "name": "Bob", "email": "bob@example.com"},
		{"id": "user-3", "name": "Alice", "email": "alice2@example.com"},
	}

	for _, item := range items {
		err = store.Insert("users", item)
		require.NoError(t, err)
	}

	results, err := store.Where("users", "email", "alice@example.com")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "user-1", results[0]["id"])
}

func TestWhereNonIndexedField(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	items := []map[string]any{
		{"id": "user-1", "name": "Alice"},
		{"id": "user-2", "name": "Bob"},
		{"id": "user-3", "name": "Alice"},
	}

	for _, item := range items {
		err = store.Insert("users", item)
		require.NoError(t, err)
	}

	results, err := store.Where("users", "name", "Alice")
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestWhereNonexistentField(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	_, err = store.Where("users", "nonexistent", "value")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestUpdate(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"id":   "user-1",
		"name": "Alice",
	}

	err = store.Insert("users", item)
	require.NoError(t, err)

	updated := map[string]any{
		"name": "Alice Updated",
	}

	err = store.Update("users", "user-1", updated)
	require.NoError(t, err)

	retrieved, err := store.Get("users", "user-1")
	require.NoError(t, err)
	require.Equal(t, "Alice Updated", retrieved["name"])
}

func TestUpdateNotFound(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"id": "user-1",
	}

	err = store.Update("users", "nonexistent", item)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestDelete(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	item := map[string]any{
		"id":   "user-1",
		"name": "Alice",
	}

	err = store.Insert("users", item)
	require.NoError(t, err)

	err = store.Delete("users", "user-1")
	require.NoError(t, err)

	_, err = store.Get("users", "user-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestDeleteNotFound(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	err = store.Delete("users", "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestConcurrentAccess(t *testing.T) {
	store := NewStore()

	schema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	err := store.CreateTable("users", schema)
	require.NoError(t, err)

	var wg sync.WaitGroup
	workers := 10
	itemsPerWorker := 10

	// Concurrent inserts
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				item := map[string]any{
					"id":   fmt.Sprintf("user-%d-%d", workerID, j),
					"name": fmt.Sprintf("User %d-%d", workerID, j),
				}
				err := store.Insert("users", item)
				require.NoError(t, err)
			}
		}(i)
	}
	wg.Wait()

	// Verify all items were inserted
	list, err := store.List("users")
	require.NoError(t, err)
	require.Len(t, list, workers*itemsPerWorker)

	// Concurrent reads
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				id := fmt.Sprintf("user-%d-%d", workerID, j)
				_, err := store.Get("users", id)
				require.NoError(t, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent updates and reads
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		// Updates
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				id := fmt.Sprintf("user-%d-%d", workerID, j)
				updated := map[string]any{
					"name": fmt.Sprintf("Updated %d-%d", workerID, j),
				}
				err := store.Update("users", id, updated)
				require.NoError(t, err)
			}
		}(i)

		// Reads
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				id := fmt.Sprintf("user-%d-%d", workerID, j)
				_, err := store.Get("users", id)
				require.NoError(t, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestMultipleTables(t *testing.T) {
	store := NewStore()

	usersSchema := Schema{
		Name: "users",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString},
		},
	}

	ordersSchema := Schema{
		Name: "orders",
		Fields: []Field{
			{Name: "id", Type: FieldTypeString, PrimaryKey: true},
			{Name: "user_id", Type: FieldTypeString, Index: true},
			{Name: "total", Type: FieldTypeFloat},
		},
	}

	err := store.CreateTable("users", usersSchema)
	require.NoError(t, err)

	err = store.CreateTable("orders", ordersSchema)
	require.NoError(t, err)

	// Insert into users
	user := map[string]any{
		"id":   "user-1",
		"name": "Alice",
	}
	err = store.Insert("users", user)
	require.NoError(t, err)

	// Insert into orders
	order := map[string]any{
		"id":      "order-1",
		"user_id": "user-1",
		"total":   99.99,
	}
	err = store.Insert("orders", order)
	require.NoError(t, err)

	// Verify both tables work
	retrievedUser, err := store.Get("users", "user-1")
	require.NoError(t, err)
	require.Equal(t, "Alice", retrievedUser["name"])

	retrievedOrder, err := store.Get("orders", "order-1")
	require.NoError(t, err)
	require.Equal(t, "user-1", retrievedOrder["user_id"])

	// Query orders by user_id
	userOrders, err := store.Where("orders", "user_id", "user-1")
	require.NoError(t, err)
	require.Len(t, userOrders, 1)
}
