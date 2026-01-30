package config

import "github.com/hashicorp/hcl/v2"

// ResourceConfig defines a resource that auto-generates REST endpoints
type ResourceConfig struct {
	Name   string         `hcl:"name,label"`
	Rows   int            `hcl:"rows,optional"`
	Seed   *int64         `hcl:"seed,optional"`
	Fields []*FieldConfig `hcl:"field,block"`
	Body   hcl.Body       `hcl:",remain"`
}

// FieldConfig defines a field in a resource
type FieldConfig struct {
	Name   string            `hcl:"name,label"`
	Type   string            `hcl:"type"`
	Config map[string]any    `hcl:"config,optional"`
	Min    *float64          `hcl:"min,optional"`
	Max    *float64          `hcl:"max,optional"`
	Values []string          `hcl:"values,optional"`
	Body   hcl.Body          `hcl:",remain"`
}
