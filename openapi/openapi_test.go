package openapi

import (
	"strings"
	"testing"
)

func TestGenerate_BasicSpec(t *testing.T) {
	cfg := SpecConfig{
		Title:   "Test API",
		Version: "1",
		Servers: []Server{{URL: "http://localhost:8080", Description: "Dev"}},
	}

	routes := []Route{
		{
			Method: "GET", Path: "/api/v1/health",
			Summary: "Health check", Auth: false,
			Response: &Response{Status: 200, Description: "OK"},
		},
	}

	spec := Generate(cfg, routes, nil)

	if spec["openapi"] != "3.1.0" {
		t.Fatalf("expected openapi 3.1.0, got %v", spec["openapi"])
	}

	info := spec["info"].(map[string]any)
	if info["title"] != "Test API" {
		t.Fatalf("expected Test API, got %v", info["title"])
	}

	paths := spec["paths"].(map[string]any)
	if _, ok := paths["/api/v1/health"]; !ok {
		t.Fatal("expected /api/v1/health path")
	}
}

func TestGenerate_WithSchemas(t *testing.T) {
	schemas := []ComponentSchema{
		{"User", Obj(map[string]Schema{
			"id":    Str("User ID"),
			"email": Str("Email"),
		})},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, nil, schemas)

	components := spec["components"].(map[string]any)
	schemasMap := components["schemas"].(map[string]any)

	if _, ok := schemasMap["User"]; !ok {
		t.Fatal("expected User schema")
	}
	if _, ok := schemasMap["Error"]; !ok {
		t.Fatal("expected Error schema (always included)")
	}
}

func TestGenerate_AuthRoute(t *testing.T) {
	routes := []Route{
		{
			Method: "GET", Path: "/api/v1/me",
			Summary: "Get me", Auth: true,
			Response: &Response{Status: 200, Description: "OK", Schema: &Schema{Ref: "User"}},
		},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, routes, nil)
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v1/me"].(map[string]any)
	get := path["get"].(map[string]any)

	// Auth route should NOT have security: [] override
	if _, ok := get["security"]; ok {
		t.Fatal("auth route should not override global security")
	}
}

func TestGenerate_PublicRoute(t *testing.T) {
	routes := []Route{
		{
			Method: "GET", Path: "/api/health",
			Summary: "Health", Auth: false,
		},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, routes, nil)
	paths := spec["paths"].(map[string]any)
	path := paths["/api/health"].(map[string]any)
	get := path["get"].(map[string]any)

	// Public route should have security: [] (empty array)
	sec, ok := get["security"].([]any)
	if !ok || len(sec) != 0 {
		t.Fatal("public route should have empty security array")
	}
}

func TestGenerate_RequestBody(t *testing.T) {
	routes := []Route{
		{
			Method: "POST", Path: "/api/v1/events",
			Summary: "Track events", Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"events": Arr(Ref("Event")),
			}, "events")},
			Response: &Response{Status: 200, Description: "Tracked"},
		},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, routes, nil)
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v1/events"].(map[string]any)
	post := path["post"].(map[string]any)

	reqBody := post["requestBody"].(map[string]any)
	if reqBody["required"] != true {
		t.Fatal("expected required request body")
	}
}

func TestGenerate_Parameters(t *testing.T) {
	routes := []Route{
		{
			Method: "GET", Path: "/api/v1/chat",
			Summary: "Chat history", Auth: true,
			Parameters: []Parameter{
				{Name: "limit", In: "query", Schema: Int("")},
				{Name: "offset", In: "query", Schema: Int("")},
			},
		},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, routes, nil)
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v1/chat"].(map[string]any)
	get := path["get"].(map[string]any)

	params := get["parameters"].([]map[string]any)
	if len(params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(params))
	}
}

func TestGenerateYAML(t *testing.T) {
	cfg := SpecConfig{Title: "Test API", Version: "1"}
	yaml := GenerateYAML(cfg, nil, nil)

	if !strings.Contains(yaml, "openapi: 3.1.0") {
		t.Fatal("expected openapi version in YAML")
	}
	if !strings.Contains(yaml, "title: Test API") {
		t.Fatal("expected title in YAML")
	}
}

func TestAllRoutes_NotEmpty(t *testing.T) {
	routes := AllRoutes()
	if len(routes) < 15 {
		t.Fatalf("expected at least 15 routes from all packages, got %d", len(routes))
	}
}

func TestAllSchemas_NotEmpty(t *testing.T) {
	schemas := AllSchemas()
	if len(schemas) < 8 {
		t.Fatalf("expected at least 8 schemas, got %d", len(schemas))
	}
}

func TestSchemaHelpers(t *testing.T) {
	s := Str("test")
	if s.Type != "string" {
		t.Fatal("Str should create string type")
	}

	s = Int("test")
	if s.Type != "integer" {
		t.Fatal("Int should create integer type")
	}

	s = Bool("test")
	if s.Type != "boolean" {
		t.Fatal("Bool should create boolean type")
	}

	s = Ref("User")
	if s.Ref != "User" {
		t.Fatal("Ref should set ref name")
	}

	s = Arr(Str(""))
	if s.Type != "array" || s.Items == nil {
		t.Fatal("Arr should create array with items")
	}

	s = StrEnum("", "a", "b")
	if len(s.Enum) != 2 {
		t.Fatal("StrEnum should set enum values")
	}

	s = NullStr("")
	if !s.Nullable {
		t.Fatal("NullStr should be nullable")
	}

	s = IntRange("", 0, 23)
	if *s.Minimum != 0 || *s.Maximum != 23 {
		t.Fatal("IntRange should set min/max")
	}
}

func TestMultipleMethodsSamePath(t *testing.T) {
	routes := []Route{
		{Method: "GET", Path: "/api/v1/items", Summary: "List items"},
		{Method: "POST", Path: "/api/v1/items", Summary: "Create item"},
	}

	spec := Generate(SpecConfig{Title: "Test", Version: "1"}, routes, nil)
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v1/items"].(map[string]any)

	if _, ok := path["get"]; !ok {
		t.Fatal("expected GET method")
	}
	if _, ok := path["post"]; !ok {
		t.Fatal("expected POST method")
	}
}

func TestGenerateYAML_FullSpec(t *testing.T) {
	cfg := SpecConfig{
		Title:   "My App API",
		Version: "1",
		Servers: []Server{{URL: "http://localhost:8080", Description: "Dev"}},
	}

	yaml := GenerateYAML(cfg, AllRoutes(), AllSchemas())

	// Verify key sections exist
	checks := []string{
		"openapi: 3.1.0",
		"/api/v1/auth/apple",
		"/api/v1/chat",
		"/api/v1/sync/changes",
		"/api/v1/paywall/config",
		"bearerAuth",
		"User",
		"ChatMessage",
		"Subscription",
	}

	for _, check := range checks {
		if !strings.Contains(yaml, check) {
			t.Fatalf("expected %q in generated YAML", check)
		}
	}
}
