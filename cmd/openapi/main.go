// Command openapi generates an OpenAPI 3.1 spec from donkeygo package route definitions.
//
// Usage:
//
//	go run github.com/pacosw1/donkeygo/cmd/openapi > openapi.yaml
//	go run github.com/pacosw1/donkeygo/cmd/openapi --title "My App" --version 2 > openapi-v2.yaml
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pacosw1/donkeygo/openapi"
)

func main() {
	title := flag.String("title", "App API", "API title")
	desc := flag.String("description", "Backend API for iOS app. Generated from donkeygo packages.", "API description")
	version := flag.String("version", "1", "API version")
	devURL := flag.String("dev-url", "http://localhost:8080", "Development server URL")
	prodURL := flag.String("prod-url", "", "Production server URL (optional)")

	flag.Parse()

	servers := []openapi.Server{
		{URL: *devURL, Description: "Local development"},
	}
	if *prodURL != "" {
		servers = append(servers, openapi.Server{URL: *prodURL, Description: "Production"})
	}

	cfg := openapi.SpecConfig{
		Title:       *title,
		Description: *desc,
		Version:     *version,
		Servers:     servers,
	}

	// Health check route (not in any package)
	cfg.ExtraRoutes = []openapi.Route{
		{
			Method: "GET", Path: "/api/health",
			Summary: "Health check", Auth: false,
			Response: &openapi.Response{Status: 200, Description: "Server is healthy"},
		},
	}

	yaml := openapi.GenerateYAML(cfg, openapi.AllRoutes(), openapi.AllSchemas())
	fmt.Fprint(os.Stdout, yaml)
}
