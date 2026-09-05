package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

func pathRoot(name string) path.Path { return path.Root(name) }

// clientFromResourceConfigure pulls the shared client out of a Configure
// request. ProviderData is nil during early framework calls, which is not an
// error: the framework calls Configure again once the provider is ready.
func clientFromResourceConfigure(req resource.ConfigureRequest, diags *diag.Diagnostics) *dataiku.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*dataiku.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *dataiku.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return nil
	}
	return client
}

func clientFromDataSourceConfigure(req datasource.ConfigureRequest, diags *diag.Diagnostics) *dataiku.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*dataiku.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *dataiku.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return nil
	}
	return client
}

// stringFromMap reads a string field out of a raw DSS document.
func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// boolFromMap reads a bool field out of a raw DSS document.
func boolFromMap(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// stringSliceFromMap reads a list-of-strings field out of a raw DSS document.
func stringSliceFromMap(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// toStringList converts a Go slice into a framework list. A nil slice becomes
// an empty list rather than null so that plans stay consistent with what the
// Create response wrote to state.
func toStringList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if values == nil {
		values = []string{}
	}
	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return list
}

// fromStringList converts a framework list into a Go slice. A null or unknown
// list becomes an empty slice.
func fromStringList(ctx context.Context, list types.List, diags *diag.Diagnostics) []string {
	if list.IsNull() || list.IsUnknown() {
		return []string{}
	}
	out := []string{}
	diags.Append(list.ElementsAs(ctx, &out, false)...)
	return out
}

// nullIfEmpty keeps optional string attributes null when DSS returns "", so
// that omitting them in configuration does not read back as perpetual drift.
func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// writeOnlyString reads a write-only attribute out of the configuration.
//
// Write-only values never appear in the plan or in state — that is the point of
// them — so the usual plan model holds null and the value has to come from the
// config directly. fallback is returned when the write-only attribute is unset,
// which is how the legacy non-write-only attribute keeps working.
func writeOnlyString(ctx context.Context, config tfsdk.Config, attribute string, fallback types.String, diags *diag.Diagnostics) string {
	var value types.String
	diags.Append(config.GetAttribute(ctx, path.Root(attribute), &value)...)
	if diags.HasError() {
		return ""
	}
	if !value.IsNull() && !value.IsUnknown() && value.ValueString() != "" {
		return value.ValueString()
	}
	return fallback.ValueString()
}
