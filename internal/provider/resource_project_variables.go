package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*projectVariablesResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectVariablesResource)(nil)
	_ resource.ResourceWithImportState = (*projectVariablesResource)(nil)
)

// NewProjectVariablesResource returns the dataiku_project_variables resource.
func NewProjectVariablesResource() resource.Resource { return &projectVariablesResource{} }

type projectVariablesResource struct {
	client *dataiku.Client
}

type projectVariablesResourceModel struct {
	ID         types.String         `tfsdk:"id"`
	ProjectKey types.String         `tfsdk:"project_key"`
	Standard   jsontypes.Normalized `tfsdk:"standard"`
	Local      jsontypes.Normalized `tfsdk:"local"`
}

func (r *projectVariablesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_variables"
}

func (r *projectVariablesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The variables of a Dataiku DSS project.\n\n" +
			"Variables are arbitrary JSON documents, so they are supplied with `jsonencode()` rather than " +
			"as typed attributes. This resource is authoritative for both scopes: it replaces the project's " +
			"whole variable document, so a variable added in the DSS interface is removed on the next apply. " +
			"Use at most one `dataiku_project_variables` resource per project.\n\n" +
			"Destroying this resource resets both scopes to an empty object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The project key. Same value as `project_key`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Key of the project to set variables on. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"standard": schema.StringAttribute{
				Optional:   true,
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				PlanModifiers: []planmodifier.String{
					suppressEquivalentJSONPlanModifier(),
				},
				MarkdownDescription: "Standard variables, as a JSON object. These are versioned with the " +
					"project and travel with a project bundle. Defaults to an empty object.",
			},
			"local": schema.StringAttribute{
				Optional:   true,
				Computed:   true,
				Sensitive:  true,
				CustomType: jsontypes.NormalizedType{},
				PlanModifiers: []planmodifier.String{
					suppressEquivalentJSONPlanModifier(),
				},
				MarkdownDescription: "Local variables, as a JSON object. These stay on the instance and are " +
					"not exported with a project bundle, which makes them the right place for per-environment " +
					"values. Marked sensitive because they commonly hold credentials. Defaults to an empty object.",
			},
		},
	}
}

func (r *projectVariablesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *projectVariablesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectVariablesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectVariablesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectVariablesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	vars, err := r.client.GetProjectVariables(ctx, projectKey)
	if err != nil {
		if dataiku.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read Dataiku project variables",
			fmt.Sprintf("Reading variables of project %q failed: %s", projectKey, err),
		)
		return
	}

	state.ID = types.StringValue(projectKey)
	state.Standard = encodeVariables(vars.Standard, "standard", projectKey, &resp.Diagnostics)
	state.Local = encodeVariables(vars.Local, "local", projectKey, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectVariablesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectVariablesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectVariablesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectVariablesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	err := r.client.SetProjectVariables(ctx, projectKey, dataiku.ProjectVariables{
		Standard: map[string]any{},
		Local:    map[string]any{},
	})
	if err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to clear Dataiku project variables",
			fmt.Sprintf("Clearing variables of project %q failed: %s", projectKey, err),
		)
	}
}

func (r *projectVariablesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("project_key"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *projectVariablesResource) write(ctx context.Context, plan *projectVariablesResourceModel, diags *diag.Diagnostics) {
	projectKey := plan.ProjectKey.ValueString()

	standard := decodeVariables(plan.Standard, "standard", diags)
	local := decodeVariables(plan.Local, "local", diags)
	if diags.HasError() {
		return
	}

	err := r.client.SetProjectVariables(ctx, projectKey, dataiku.ProjectVariables{
		Standard: standard,
		Local:    local,
	})
	if err != nil {
		diags.AddError(
			"Unable to set Dataiku project variables",
			fmt.Sprintf("Setting variables of project %q failed: %s", projectKey, err),
		)
		return
	}

	plan.ID = types.StringValue(projectKey)
	plan.Standard = encodeVariables(standard, "standard", projectKey, diags)
	plan.Local = encodeVariables(local, "local", projectKey, diags)
}

// decodeVariables turns a configured JSON document into a map. A null value
// means an empty document rather than "leave alone", because this resource is
// authoritative over both scopes.
func decodeVariables(value jsontypes.Normalized, attribute string, diags *diag.Diagnostics) map[string]any {
	if value.IsNull() || value.IsUnknown() {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(value.ValueString()), &out); err != nil {
		diags.AddAttributeError(
			pathRoot(attribute),
			"Invalid project variables",
			fmt.Sprintf("%s must be a JSON object: %s", attribute, err),
		)
		return nil
	}
	return out
}

func encodeVariables(values map[string]any, attribute, projectKey string, diags *diag.Diagnostics) jsontypes.Normalized {
	if values == nil {
		values = map[string]any{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		diags.AddError(
			"Unable to encode Dataiku project variables",
			fmt.Sprintf("Encoding the %s variables of project %q failed: %s", attribute, projectKey, err),
		)
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(encoded))
}
