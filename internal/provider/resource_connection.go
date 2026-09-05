package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*connectionResource)(nil)
	_ resource.ResourceWithConfigure   = (*connectionResource)(nil)
	_ resource.ResourceWithImportState = (*connectionResource)(nil)
)

// NewConnectionResource returns the dataiku_connection resource.
func NewConnectionResource() resource.Resource { return &connectionResource{} }

type connectionResource struct {
	client *dataiku.Client
}

type connectionResourceModel struct {
	ID            types.String         `tfsdk:"id"`
	Name          types.String         `tfsdk:"name"`
	Type          types.String         `tfsdk:"type"`
	Description   types.String         `tfsdk:"description"`
	ParamsJSON    jsontypes.Normalized `tfsdk:"params_json"`
	UsableBy      types.String         `tfsdk:"usable_by"`
	AllowedGroups types.List           `tfsdk:"allowed_groups"`
}

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A connection on a Dataiku DSS instance. Requires an API key with admin rights.\n\n" +
			"Connection parameters vary widely by connection type, so they are supplied as a JSON document " +
			"rather than as typed attributes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The connection name. Same value as `name`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the connection. Changing this forces a new connection.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Connection type, for example `PostgreSQL`, `Snowflake`, `S3`, `EC2` or " +
					"`Filesystem`. The accepted values depend on your DSS version and license, so this " +
					"provider does not restrict them. Changing this forces a new connection.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the connection.",
			},
			"params_json": schema.StringAttribute{
				Optional:   true,
				Sensitive:  true,
				CustomType: jsontypes.NormalizedType{},
				PlanModifiers: []planmodifier.String{
					suppressEquivalentJSONPlanModifier(),
				},
				MarkdownDescription: "Type-specific connection parameters, as a JSON object. Use " +
					"`jsonencode()` to build it. Because these commonly carry credentials, DSS redacts " +
					"secret fields when reading a connection back; this provider therefore does not refresh " +
					"`params_json` from the instance after it is set, and changes made in the DSS interface " +
					"to parameters will not appear as drift. On `terraform import` the redacted parameters " +
					"are read in once so you have a starting point to edit.",
			},
			"usable_by": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ALL"),
				MarkdownDescription: "Who may use the connection. `ALL` for everyone, or `ALLOWED` to restrict it to `allowed_groups`.",
				Validators: []validator.String{
					stringvalidator.OneOf("ALL", "ALLOWED"),
				},
			},
			"allowed_groups": schema.ListAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Groups allowed to use the connection when `usable_by` is `ALLOWED`.",
			},
		},
	}
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan connectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	params := decodeParams(plan.ParamsJSON, &resp.Diagnostics)
	allowedGroups := fromStringList(ctx, plan.AllowedGroups, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := dataiku.CreateConnectionRequest{
		Name:          name,
		Type:          plan.Type.ValueString(),
		Description:   plan.Description.ValueString(),
		Params:        params,
		UsableBy:      plan.UsableBy.ValueString(),
		AllowedGroups: allowedGroups,
	}
	if err := r.client.CreateConnection(ctx, createReq); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku connection",
			fmt.Sprintf("Creating connection %q failed: %s", name, err),
		)
		return
	}

	found, diags := r.readInto(ctx, name, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku connection disappeared after creation",
			fmt.Sprintf("The connection %q was created but could not be read back.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state connectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readInto(ctx, state.Name.ValueString(), &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan connectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	params := decodeParams(plan.ParamsJSON, &resp.Diagnostics)
	allowedGroups := fromStringList(ctx, plan.AllowedGroups, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	paramsSet := !plan.ParamsJSON.IsNull() && !plan.ParamsJSON.IsUnknown()

	err := r.client.UpdateConnection(ctx, name, func(c map[string]any) {
		c["description"] = plan.Description.ValueString()
		c["usableBy"] = plan.UsableBy.ValueString()
		c["allowedGroups"] = allowedGroups
		// Writing back the redacted params DSS returned would destroy the
		// stored secrets, so only send params when the configuration has them.
		if paramsSet {
			c["params"] = params
		}
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku connection",
			fmt.Sprintf("Updating connection %q failed: %s", name, err),
		)
		return
	}

	found, diags := r.readInto(ctx, name, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku connection disappeared during update",
			fmt.Sprintf("The connection %q could not be read back after being updated.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state connectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if err := r.client.DeleteConnection(ctx, name); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku connection",
			fmt.Sprintf("Deleting connection %q failed: %s", name, err),
		)
	}
}

func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// readInto refreshes model from the instance. params_json is only populated
// when the model does not already carry it, so that an import gets a starting
// point while a managed connection keeps the unredacted configured value.
func (r *connectionResource) readInto(ctx context.Context, name string, model *connectionResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	conn, err := r.client.GetConnection(ctx, name)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku connection",
			fmt.Sprintf("Reading connection %q failed: %s", name, err),
		)
		return false, diags
	}

	model.ID = types.StringValue(name)
	model.Name = types.StringValue(name)
	model.Type = types.StringValue(stringFromMap(conn, "type"))
	model.Description = nullIfEmpty(stringFromMap(conn, "description"))
	model.UsableBy = types.StringValue(stringFromMap(conn, "usableBy"))
	model.AllowedGroups = toStringList(ctx, stringSliceFromMap(conn, "allowedGroups"), &diags)

	if model.ParamsJSON.IsNull() || model.ParamsJSON.IsUnknown() {
		if raw, ok := conn["params"]; ok && raw != nil {
			encoded, err := json.Marshal(raw)
			if err != nil {
				diags.AddError(
					"Unable to encode connection parameters",
					fmt.Sprintf("Encoding the parameters of connection %q failed: %s", name, err),
				)
				return false, diags
			}
			model.ParamsJSON = jsontypes.NewNormalizedValue(string(encoded))
		}
	}

	return true, diags
}

// decodeParams turns the configured JSON document into a map, rejecting
// anything that is not a JSON object.
func decodeParams(value jsontypes.Normalized, diags *diag.Diagnostics) map[string]any {
	if value.IsNull() || value.IsUnknown() {
		return map[string]any{}
	}
	params := map[string]any{}
	if err := json.Unmarshal([]byte(value.ValueString()), &params); err != nil {
		diags.AddAttributeError(
			pathRoot("params_json"),
			"Invalid connection parameters",
			fmt.Sprintf("params_json must be a JSON object: %s", err),
		)
		return nil
	}
	return params
}
