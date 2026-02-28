package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/norncorp/loki/internal/config"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/renderer"
)

var pathParamRegex = regexp.MustCompile(`\{([^}]+)\}`)

// SpecHandler serves pre-generated mock responses derived from an OpenAPI spec.
type SpecHandler struct {
	routes []*specRoute
	logger *slog.Logger
}

type specRoute struct {
	method   string   // "GET", "POST", etc.
	path     string   // "/pets/:petId" (converted from {petId})
	segments []string // pre-split path segments for matching
	response []byte   // pre-generated JSON response
	status   int      // HTTP status code
}

// NewSpecHandler loads an OpenAPI 3.x spec and builds routes with pre-generated mock responses.
func NewSpecHandler(cfg *config.SpecConfig, logger *slog.Logger) (*SpecHandler, error) {
	specBytes, err := os.ReadFile(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file %q: %w", cfg.Path, err)
	}

	doc, err := libopenapi.NewDocument(specBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	v3Model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenAPI V3 model: %w", err)
	}

	if v3Model.Model.Paths == nil || v3Model.Model.Paths.PathItems == nil {
		return &SpecHandler{logger: logger}, nil
	}

	mg := renderer.NewMockGenerator(renderer.JSON)
	mg.SetPretty()
	if cfg.Seed != nil {
		mg.SetSeed(*cfg.Seed)
	}

	rows := 10
	if cfg.Rows != nil {
		rows = *cfg.Rows
	}

	var routes []*specRoute

	for path, pathItem := range v3Model.Model.Paths.PathItems.FromOldest() {
		convertedPath := pathParamRegex.ReplaceAllString(path, ":$1")

		for method, op := range pathItem.GetOperations().FromOldest() {
			if op.Responses == nil || op.Responses.Codes == nil {
				continue
			}

			// Collect and sort response codes to find the lowest 2xx
			var codes []string
			for code := range op.Responses.Codes.KeysFromOldest() {
				codes = append(codes, code)
			}
			sort.Strings(codes)

			var statusCode int
			for _, code := range codes {
				c, parseErr := strconv.Atoi(code)
				if parseErr == nil && c >= 200 && c < 300 {
					statusCode = c
					break
				}
			}
			if statusCode == 0 {
				continue
			}

			resp := op.Responses.Codes.GetOrZero(strconv.Itoa(statusCode))
			if resp == nil {
				continue
			}

			var responseBytes []byte

			if resp.Content != nil {
				jsonMedia := resp.Content.GetOrZero("application/json")
				if jsonMedia != nil && jsonMedia.Schema != nil {
					schema := jsonMedia.Schema.Schema()

					// Check if schema is an array type
					isArray := false
					if schema != nil {
						for _, t := range schema.Type {
							if t == "array" {
								isArray = true
								break
							}
						}
					}

					if isArray && schema.Items != nil && schema.Items.A != nil {
						// Array schema: generate N items from the items schema
						itemSchema := schema.Items.A.Schema()
						if itemSchema != nil {
							items := make([]json.RawMessage, 0, rows)
							for i := 0; i < rows; i++ {
								mockBytes, genErr := mg.GenerateMock(itemSchema, "")
								if genErr != nil {
									logger.Warn("failed to generate array item mock",
										"path", path, "method", method, "error", genErr)
									break
								}
								items = append(items, json.RawMessage(mockBytes))
							}
							if len(items) > 0 {
								responseBytes, _ = json.MarshalIndent(items, "", "  ")
							}
						}
					} else if schema != nil {
						// Non-array schema: generate mock from the media type
						mockBytes, genErr := mg.GenerateMock(jsonMedia, "")
						if genErr != nil {
							logger.Warn("failed to generate mock response",
								"path", path, "method", method, "error", genErr)
						} else {
							responseBytes = mockBytes
						}
					}
				}
			}

			route := &specRoute{
				method:   strings.ToUpper(method),
				path:     convertedPath,
				segments: strings.Split(convertedPath, "/"),
				response: responseBytes,
				status:   statusCode,
			}
			routes = append(routes, route)

			logger.Info("registered spec route",
				"method", route.method,
				"path", route.path,
				"status", route.status,
				"responseSize", len(route.response))
		}
	}

	return &SpecHandler{routes: routes, logger: logger}, nil
}

// Match finds a matching spec route for the given HTTP method and path.
func (sh *SpecHandler) Match(method, path string) (*specRoute, bool) {
	for _, route := range sh.routes {
		if route.method != "" && route.method != method {
			continue
		}

		// Fast path: no params, exact string match
		if !strings.Contains(route.path, ":") {
			if route.path == path {
				return route, true
			}
			continue
		}

		// Segment-by-segment matching with :param wildcard support
		reqParts := strings.Split(path, "/")
		if len(route.segments) != len(reqParts) {
			continue
		}

		matched := true
		for i, seg := range route.segments {
			if strings.HasPrefix(seg, ":") {
				continue
			}
			if seg != reqParts[i] {
				matched = false
				break
			}
		}
		if matched {
			return route, true
		}
	}
	return nil, false
}

// Handle writes the pre-generated response for a matched spec route.
func (sh *SpecHandler) Handle(w http.ResponseWriter, r *http.Request, route *specRoute) {
	if route.response != nil {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(route.status)
	if route.response != nil {
		w.Write(route.response)
	}
}
