package config

import "github.com/hashicorp/hcl/v2"

// ServiceSchema returns the HCL schema for a service block
// This will be extended in future phases as we add more service types
func ServiceSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type", Required: true},
			{Name: "listen", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "handle"},
			{Type: "resource"},
		},
	}
}

// HandlerSchema returns the HCL schema for a handler block
func HandlerSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "route"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "response"},
		},
	}
}

// ResponseSchema returns the HCL schema for a response block
func ResponseSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "status"},
			{Name: "headers"},
			{Name: "body"},
		},
	}
}
