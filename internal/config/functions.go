package config

import (
	"time"

	"github.com/google/uuid"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// Functions returns the built-in HCL functions available in config files
func Functions() map[string]function.Function {
	return map[string]function.Function{
		"jsonencode": stdlib.JSONEncodeFunc,
		"uuid":       UuidFunc,
		"timestamp":  TimestampFunc,
	}
}

// UuidFunc generates a random UUID v4
var UuidFunc = function.New(&function.Spec{
	Params: []function.Parameter{},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		id := uuid.New()
		return cty.StringVal(id.String()), nil
	},
})

// TimestampFunc returns the current timestamp in ISO 8601 format
var TimestampFunc = function.New(&function.Spec{
	Params: []function.Parameter{},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		now := time.Now().UTC().Format(time.RFC3339)
		return cty.StringVal(now), nil
	},
})
