// Package openapi provides programmatic OpenAPI 3.1 spec generation from donkeygo route definitions.
// Each donkeygo package exports Routes() and Schemas() — this package composes them into a full spec.
package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ── Route Definition Types ──────────────────────────────────────────────────

// Route describes a single API endpoint.
type Route struct {
	Method      string            // "GET", "POST", "PUT", "DELETE"
	Path        string            // "/api/v1/auth/apple"
	Summary     string            // Short description
	Description string            // Longer description (optional)
	Tags        []string          // e.g. ["auth"]
	Auth        bool              // true if requires bearer auth
	Request     *RequestBody      // nil if no request body
	Response    *Response         // primary success response
	Parameters  []Parameter       // query/path parameters
}

// RequestBody describes a request body.
type RequestBody struct {
	Required    bool
	ContentType string  // default "application/json"
	Schema      Schema
}

// Response describes a response.
type Response struct {
	Status      int     // 200, 201, etc.
	Description string
	Schema      *Schema // nil if no body
}

// Parameter describes a query or path parameter.
type Parameter struct {
	Name        string
	In          string  // "query", "path"
	Description string
	Required    bool
	Schema      Schema
}

// Schema describes a JSON schema type.
type Schema struct {
	Type        string            // "object", "array", "string", "integer", "boolean"
	Ref         string            // "$ref" to components/schemas (e.g. "User")
	Properties  map[string]Schema // for type=object
	Required    []string          // required property names
	Items       *Schema           // for type=array
	Format      string            // "date-time", "uuid", "binary"
	Enum        []string          // enum values
	Nullable    bool
	Description string
	Default     any
	Minimum     *int
	Maximum     *int
	MaxItems    *int
}

// ComponentSchema is a named schema for components/schemas section.
type ComponentSchema struct {
	Name   string
	Schema Schema
}

// ── Spec Generation ─────────────────────────────────────────────────────────

// SpecConfig configures the generated OpenAPI spec.
type SpecConfig struct {
	Title       string
	Description string
	Version     string
	Servers     []Server
	// Extra schemas beyond what packages export
	ExtraSchemas []ComponentSchema
	// Extra routes beyond what packages export
	ExtraRoutes []Route
}

// Server describes a server in the spec.
type Server struct {
	URL         string
	Description string
}

// Generate builds a complete OpenAPI 3.1 spec from route and schema definitions.
func Generate(cfg SpecConfig, routes []Route, schemas []ComponentSchema) map[string]any {
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       cfg.Title,
			"description": cfg.Description,
			"version":     cfg.Version,
		},
	}

	// Servers
	if len(cfg.Servers) > 0 {
		servers := make([]map[string]string, len(cfg.Servers))
		for i, s := range cfg.Servers {
			servers[i] = map[string]string{"url": s.URL, "description": s.Description}
		}
		spec["servers"] = servers
	}

	// Security
	spec["security"] = []map[string]any{{"bearerAuth": []string{}}}

	// Components
	components := map[string]any{
		"securitySchemes": map[string]any{
			"bearerAuth": map[string]any{
				"type":         "http",
				"scheme":       "bearer",
				"bearerFormat": "JWT",
			},
		},
		"headers": map[string]any{
			"X-Device-Token": map[string]any{
				"description": "Device identifier (APNs token for iOS)",
				"schema":      map[string]any{"type": "string"},
			},
			"X-Timezone": map[string]any{
				"description": "Client timezone (e.g., America/New_York)",
				"schema":      map[string]any{"type": "string"},
			},
		},
	}

	// Build schemas map
	allSchemas := append(schemas, cfg.ExtraSchemas...)
	schemasMap := make(map[string]any, len(allSchemas))
	// Always include Error schema
	schemasMap["Error"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"error": map[string]string{"type": "string"}},
	}
	for _, s := range allSchemas {
		schemasMap[s.Name] = schemaToMap(s.Schema)
	}
	components["schemas"] = schemasMap
	spec["components"] = components

	// Paths
	allRoutes := append(routes, cfg.ExtraRoutes...)
	paths := buildPaths(allRoutes)
	spec["paths"] = paths

	return spec
}

// GenerateYAML builds the spec and returns it as YAML string.
func GenerateYAML(cfg SpecConfig, routes []Route, schemas []ComponentSchema) string {
	spec := Generate(cfg, routes, schemas)
	return mapToYAML(spec, 0)
}

// ── Schema Conversion ───────────────────────────────────────────────────────

func schemaToMap(s Schema) map[string]any {
	if s.Ref != "" {
		return map[string]any{"$ref": "#/components/schemas/" + s.Ref}
	}

	m := map[string]any{}

	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Nullable {
		m["nullable"] = true
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Default != nil {
		m["default"] = s.Default
	}
	if s.Minimum != nil {
		m["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		m["maximum"] = *s.Maximum
	}
	if s.MaxItems != nil {
		m["maxItems"] = *s.MaxItems
	}

	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}

	if len(s.Required) > 0 {
		m["required"] = s.Required
	}

	if s.Items != nil {
		m["items"] = schemaToMap(*s.Items)
	}

	return m
}

func buildPaths(routes []Route) map[string]any {
	paths := make(map[string]any)

	// Group routes by path
	pathGroups := make(map[string][]Route)
	for _, r := range routes {
		pathGroups[r.Path] = append(pathGroups[r.Path], r)
	}

	// Sort paths for deterministic output
	sortedPaths := make([]string, 0, len(pathGroups))
	for p := range pathGroups {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		pathRoutes := pathGroups[path]
		methods := make(map[string]any)

		for _, r := range pathRoutes {
			op := map[string]any{
				"summary": r.Summary,
			}

			if r.Description != "" {
				op["description"] = r.Description
			}

			if len(r.Tags) > 0 {
				op["tags"] = r.Tags
			}

			if !r.Auth {
				op["security"] = []any{}
			}

			// Parameters
			if len(r.Parameters) > 0 {
				params := make([]map[string]any, len(r.Parameters))
				for i, p := range r.Parameters {
					param := map[string]any{
						"name":   p.Name,
						"in":     p.In,
						"schema": schemaToMap(p.Schema),
					}
					if p.Description != "" {
						param["description"] = p.Description
					}
					if p.Required {
						param["required"] = true
					}
					params[i] = param
				}
				op["parameters"] = params
			}

			// Request body
			if r.Request != nil {
				ct := r.Request.ContentType
				if ct == "" {
					ct = "application/json"
				}
				reqBody := map[string]any{
					"content": map[string]any{
						ct: map[string]any{
							"schema": schemaToMap(r.Request.Schema),
						},
					},
				}
				if r.Request.Required {
					reqBody["required"] = true
				}
				op["requestBody"] = reqBody
			}

			// Response
			resp := map[string]any{}
			if r.Response != nil {
				status := fmt.Sprintf("%d", r.Response.Status)
				respObj := map[string]any{
					"description": r.Response.Description,
				}
				if r.Response.Schema != nil {
					respObj["content"] = map[string]any{
						"application/json": map[string]any{
							"schema": schemaToMap(*r.Response.Schema),
						},
					}
				}
				resp[status] = respObj
			} else {
				resp["200"] = map[string]any{"description": "Success"}
			}
			op["responses"] = resp

			methods[strings.ToLower(r.Method)] = op
		}

		paths[path] = methods
	}

	return paths
}

// ── YAML Serialization (no external dependency) ─────────────────────────────

// yamlKeyOrder returns a sort priority for common OpenAPI keys.
func yamlKeyOrder(key string) int {
	order := map[string]int{
		"openapi": 0, "info": 1, "servers": 2, "security": 3,
		"components": 4, "paths": 5,
		"title": 10, "description": 11, "version": 12,
		"type": 20, "format": 21, "enum": 22, "properties": 23,
		"required": 24, "items": 25, "nullable": 26,
		"summary": 30, "tags": 31, "parameters": 32,
		"requestBody": 33, "responses": 34,
		"$ref": 5,
	}
	if n, ok := order[key]; ok {
		return n
	}
	return 50
}

func mapToYAML(v any, indent int) string {
	var sb strings.Builder
	writeYAML(&sb, v, indent, false)
	return sb.String()
}

func writeYAML(sb *strings.Builder, v any, indent int, inArray bool) {
	prefix := strings.Repeat("  ", indent)

	switch val := v.(type) {
	case map[string]any:
		// Sort keys — prioritize OpenAPI key order, then alphabetical
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			oi, oj := yamlKeyOrder(keys[i]), yamlKeyOrder(keys[j])
			if oi != oj {
				return oi < oj
			}
			return keys[i] < keys[j]
		})

		for i, k := range keys {
			if i > 0 || !inArray {
				sb.WriteString(prefix)
			}
			sb.WriteString(k)
			sb.WriteString(":")

			child := val[k]
			switch child.(type) {
			case map[string]any:
				sb.WriteString("\n")
				writeYAML(sb, child, indent+1, false)
			case []any:
				if len(child.([]any)) == 0 {
					sb.WriteString(" []\n")
				} else {
					sb.WriteString("\n")
					writeYAML(sb, child, indent+1, false)
				}
			case []string:
				sb.WriteString("\n")
				writeYAML(sb, child, indent+1, false)
			case []map[string]any:
				sb.WriteString("\n")
				writeYAML(sb, child, indent+1, false)
			case []map[string]string:
				sb.WriteString("\n")
				writeYAML(sb, child, indent+1, false)
			default:
				sb.WriteString(" ")
				writeYAMLScalar(sb, child)
				sb.WriteString("\n")
			}
		}

	case []any:
		for _, item := range val {
			sb.WriteString(prefix)
			sb.WriteString("- ")
			switch item.(type) {
			case map[string]any:
				writeYAML(sb, item, indent+1, true)
			default:
				writeYAMLScalar(sb, item)
				sb.WriteString("\n")
			}
		}

	case []string:
		for _, item := range val {
			sb.WriteString(prefix)
			sb.WriteString("- ")
			sb.WriteString(item)
			sb.WriteString("\n")
		}

	case []map[string]any:
		for _, item := range val {
			sb.WriteString(prefix)
			sb.WriteString("- ")
			writeYAML(sb, any(item), indent+1, true)
		}

	case []map[string]string:
		for _, item := range val {
			sb.WriteString(prefix)
			sb.WriteString("- ")
			first := true
			for k, v := range item {
				if !first {
					sb.WriteString(prefix)
					sb.WriteString("  ")
				}
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(v)
				sb.WriteString("\n")
				first = false
			}
		}

	default:
		writeYAMLScalar(sb, v)
		sb.WriteString("\n")
	}
}

func writeYAMLScalar(sb *strings.Builder, v any) {
	switch val := v.(type) {
	case string:
		// Quote strings that contain special chars or look like booleans/numbers
		if val == "" || val == "true" || val == "false" || strings.ContainsAny(val, ":#{}[]|>&*!%@`'\"\\,\n") {
			sb.WriteString("\"")
			sb.WriteString(strings.ReplaceAll(val, "\"", "\\\""))
			sb.WriteString("\"")
		} else {
			sb.WriteString(val)
		}
	case bool:
		if val {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		sb.WriteString(fmt.Sprintf("%d", val))
	case float64:
		sb.WriteString(fmt.Sprintf("%g", val))
	case nil:
		sb.WriteString("null")
	default:
		// Fallback to JSON encoding
		data, _ := json.Marshal(val)
		sb.Write(data)
	}
}
