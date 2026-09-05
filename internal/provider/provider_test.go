package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// TestProviderSchema checks that the provider's own schema is valid.
func TestProviderSchema(t *testing.T) {
	ctx := context.Background()
	resp := &provider.SchemaResponse{}
	New("test")().Schema(ctx, provider.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("provider schema is not implementable: %v", diags)
	}

	for _, name := range []string{"host", "api_key", "insecure", "timeout_seconds", "max_retries"} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("provider schema is missing the %q attribute", name)
		}
	}
	if !resp.Schema.Attributes["api_key"].IsSensitive() {
		t.Error("api_key must be marked sensitive")
	}
}

// TestResourceSchemas walks every registered resource and validates its
// schema, its type name, and that it can be imported.
func TestResourceSchemas(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	factories := p.Resources(ctx)
	if len(factories) == 0 {
		t.Fatal("the provider registers no resources")
	}

	seen := map[string]bool{}
	for _, factory := range factories {
		r := factory()

		metaResp := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "dataiku"}, metaResp)
		name := metaResp.TypeName

		t.Run(name, func(t *testing.T) {
			if !strings.HasPrefix(name, "dataiku_") {
				t.Errorf("type name %q does not start with dataiku_", name)
			}
			if seen[name] {
				t.Fatalf("type name %q is registered twice", name)
			}
			seen[name] = true

			schemaResp := &resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
			if schemaResp.Diagnostics.HasError() {
				t.Fatalf("schema: %v", schemaResp.Diagnostics)
			}
			if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
				t.Fatalf("schema is not implementable: %v", diags)
			}

			assertHasIDAttribute(t, schemaResp.Schema)
			assertDescribed(t, name, schemaResp.Schema)

			if _, ok := r.(resource.ResourceWithImportState); !ok {
				t.Error("resource does not support terraform import")
			}
			if _, ok := r.(resource.ResourceWithConfigure); !ok {
				t.Error("resource does not implement Configure, so it can never get a client")
			}
		})
	}
}

// TestDataSourceSchemas does the same for data sources.
func TestDataSourceSchemas(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	factories := p.DataSources(ctx)
	if len(factories) == 0 {
		t.Fatal("the provider registers no data sources")
	}

	seen := map[string]bool{}
	for _, factory := range factories {
		d := factory()

		metaResp := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "dataiku"}, metaResp)
		name := metaResp.TypeName

		t.Run(name, func(t *testing.T) {
			if !strings.HasPrefix(name, "dataiku_") {
				t.Errorf("type name %q does not start with dataiku_", name)
			}
			if seen[name] {
				t.Fatalf("type name %q is registered twice", name)
			}
			seen[name] = true

			schemaResp := &datasource.SchemaResponse{}
			d.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
			if schemaResp.Diagnostics.HasError() {
				t.Fatalf("schema: %v", schemaResp.Diagnostics)
			}
			if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
				t.Fatalf("schema is not implementable: %v", diags)
			}

			if schemaResp.Schema.MarkdownDescription == "" {
				t.Error("data source has no MarkdownDescription")
			}
			if _, ok := d.(datasource.DataSourceWithConfigure); !ok {
				t.Error("data source does not implement Configure, so it can never get a client")
			}
		})
	}
}

// TestExpectedResourcesAreRegistered guards against a resource being written
// but never wired into the provider.
func TestExpectedResourcesAreRegistered(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	got := map[string]bool{}
	for _, factory := range p.Resources(ctx) {
		resp := &resource.MetadataResponse{}
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "dataiku"}, resp)
		got[resp.TypeName] = true
	}

	want := []string{
		"dataiku_project",
		"dataiku_project_permissions",
		"dataiku_project_variables",
		"dataiku_project_folder",
		"dataiku_user",
		"dataiku_group",
		"dataiku_connection",
		"dataiku_code_env",
		"dataiku_scenario",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("resource %q is not registered", name)
		}
	}

	gotDS := map[string]bool{}
	for _, factory := range p.DataSources(ctx) {
		resp := &datasource.MetadataResponse{}
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "dataiku"}, resp)
		gotDS[resp.TypeName] = true
	}
	for _, name := range []string{
		"dataiku_project",
		"dataiku_projects",
		"dataiku_user",
		"dataiku_group",
		"dataiku_connection",
		"dataiku_project_folder",
	} {
		if !gotDS[name] {
			t.Errorf("data source %q is not registered", name)
		}
	}
}

// TestSensitiveAttributes pins the attributes that must never be shown in
// plan output, so a future edit cannot quietly unmark one.
func TestSensitiveAttributes(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	sensitive := map[string][]string{
		"dataiku_user":              {"password"},
		"dataiku_connection":        {"params_json"},
		"dataiku_project_variables": {"local"},
	}

	for _, factory := range p.Resources(ctx) {
		r := factory()
		metaResp := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "dataiku"}, metaResp)

		want, ok := sensitive[metaResp.TypeName]
		if !ok {
			continue
		}

		schemaResp := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, schemaResp)

		for _, name := range want {
			attr, ok := schemaResp.Schema.Attributes[name]
			if !ok {
				t.Errorf("%s: attribute %q is missing", metaResp.TypeName, name)
				continue
			}
			if !attr.IsSensitive() {
				t.Errorf("%s: attribute %q must be marked sensitive", metaResp.TypeName, name)
			}
		}
	}
}

func assertHasIDAttribute(t *testing.T, s fwresource.Schema) {
	t.Helper()
	attr, ok := s.Attributes["id"]
	if !ok {
		t.Error("resource has no id attribute")
		return
	}
	if !attr.IsComputed() {
		t.Error("the id attribute must be computed")
	}
}

func assertDescribed(t *testing.T, name string, s fwresource.Schema) {
	t.Helper()
	if s.MarkdownDescription == "" {
		t.Errorf("%s has no MarkdownDescription", name)
	}
	for attrName, attr := range s.Attributes {
		if attr.GetMarkdownDescription() == "" && attr.GetDescription() == "" {
			t.Errorf("%s: attribute %q has no description", name, attrName)
		}
	}
}
