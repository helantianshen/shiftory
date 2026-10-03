package api

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestOpenAPIDocumentIsValidYAMLAndCoversPublicRoutes 检查接口文档能解析为 YAML 且包含公开路由
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
	if len(document.Paths) != 45 {
		t.Fatalf("expected all 45 route groups, got %d", len(document.Paths))
	}
}
