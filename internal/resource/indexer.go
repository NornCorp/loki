package resource

import (
	"fmt"

	"github.com/hashicorp/go-memdb"
)

// MapFieldIndexer is a custom indexer for map[string]any fields
type MapFieldIndexer struct {
	Field string
}

// FromObject extracts the indexed field from a map
func (m *MapFieldIndexer) FromObject(obj interface{}) (bool, []byte, error) {
	item, ok := obj.(map[string]any)
	if !ok {
		return false, nil, fmt.Errorf("object is not a map")
	}

	val, exists := item[m.Field]
	if !exists {
		return false, nil, nil
	}

	// Convert value to string for indexing
	var str string
	switch v := val.(type) {
	case string:
		str = v
	case int:
		str = fmt.Sprintf("%d", v)
	case int64:
		str = fmt.Sprintf("%d", v)
	case float64:
		str = fmt.Sprintf("%f", v)
	case bool:
		str = fmt.Sprintf("%t", v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	return true, []byte(str), nil
}

// FromArgs converts lookup arguments to index bytes
func (m *MapFieldIndexer) FromArgs(args ...interface{}) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("must provide exactly one argument")
	}

	// Convert argument to string
	var str string
	switch v := args[0].(type) {
	case string:
		str = v
	case int:
		str = fmt.Sprintf("%d", v)
	case int64:
		str = fmt.Sprintf("%d", v)
	case float64:
		str = fmt.Sprintf("%f", v)
	case bool:
		str = fmt.Sprintf("%t", v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	return []byte(str), nil
}

// PrefixFromArgs is used for prefix-based queries
func (m *MapFieldIndexer) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return m.FromArgs(args...)
}

var _ memdb.Indexer = (*MapFieldIndexer)(nil)
var _ memdb.SingleIndexer = (*MapFieldIndexer)(nil)
var _ memdb.PrefixIndexer = (*MapFieldIndexer)(nil)
