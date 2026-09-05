package api

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestOpenAPIDocumentIsValidYAMLAndCoversPublicRoutes(t *testing.T) {
	content, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse OpenAPI YAML: %v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("expected OpenAPI 3.1.0, got %q", document.OpenAPI)
	}
	if len(document.Paths) != 35 {
		t.Fatalf("expected all 35 route groups, got %d", len(document.Paths))
	}
}
