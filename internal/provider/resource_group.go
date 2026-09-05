package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*groupResource)(nil)
	_ resource.ResourceWithConfigure   = (*groupResource)(nil)
	_ resource.ResourceWithImportState = (*groupResource)(nil)
)

// NewGroupResource returns the dataiku_group resource.
func NewGroupResource() resource.Resource { return &groupResource{} }

type groupResource struct {
	client *dataiku.Client
}

type groupResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	SourceType     types.String `tfsdk:"source_type"`
	Admin          types.Bool   `tfsdk:"admin"`
	LDAPGroupNames types.String `tfsdk:"ldap_group_names"`
	Permissions    types.Map    `tfsdk:"permissions"`
}

func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A group on a Dataiku DSS instance. Requires an API key with admin rights.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The group name. Same value as `name`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the group. Changing this forces a new group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the group.",
			},
			"source_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("LOCAL"),
				MarkdownDescription: "Where the group is defined. One of `LOCAL` or `LDAP`. Defaults to `LOCAL`. Changing this forces a new group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("LOCAL", "LDAP"),
				},
			},
			"admin": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether members of this group are DSS administrators.",
			},
			"ldap_group_names": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comma-separated LDAP group names mapped to this group. Only meaningful when `source_type` is `LDAP`.",
			},
			"permissions": schema.MapAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.BoolType,
				MarkdownDescription: "Global abilities granted to the group, keyed by the raw DSS field name " +
					"(for example `mayCreateProjects`). The set of abilities differs between DSS versions and " +
					"editions, so this provider passes them through rather than modelling a fixed list. To see " +
					"the names your instance supports, read an existing group:\n\n" +
					"```\ncurl -u $DATAIKU_API_KEY: https://dss.example.com/public/api/admin/groups/administrators\n```\n\n" +
					"Abilities absent from this map are left at whatever the instance already has, which keeps " +
					"a DSS upgrade from silently revoking an ability this provider version does not know about.",
			},
		},
	}
}

func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()

	createReq := dataiku.CreateGroupRequest{
		Name:        name,
		Description: plan.Description.ValueString(),
		SourceType:  plan.SourceType.ValueString(),
	}
	if err := r.client.CreateGroup(ctx, createReq); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku group",
			fmt.Sprintf("Creating group %q failed: %s", name, err),
		)
		return
	}

	// Abilities and the LDAP mapping are not part of the creation payload.
	if err := r.applyDefinition(ctx, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Unable to set Dataiku group permissions",
			fmt.Sprintf("The group %q was created but applying its settings failed: %s", name, err),
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
			"Dataiku group disappeared after creation",
			fmt.Sprintf("The group %q was created but could not be read back.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupResourceModel
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

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	if err := r.applyDefinition(ctx, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku group",
			fmt.Sprintf("Updating group %q failed: %s", name, err),
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
			"Dataiku group disappeared during update",
			fmt.Sprintf("The group %q could not be read back after being updated.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if err := r.client.DeleteGroup(ctx, name); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku group",
			fmt.Sprintf("Deleting group %q failed: %s", name, err),
		)
	}
}

func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// applyDefinition writes the managed fields onto the group's current
// definition, leaving every other field the instance reports untouched.
func (r *groupResource) applyDefinition(ctx context.Context, plan *groupResourceModel) error {
	permissions := map[string]bool{}
	if !plan.Permissions.IsNull() && !plan.Permissions.IsUnknown() {
		if diags := plan.Permissions.ElementsAs(ctx, &permissions, false); diags.HasError() {
			return fmt.Errorf("reading the permissions map: %v", diags.Errors())
		}
	}

	return r.client.UpdateGroup(ctx, plan.Name.ValueString(), func(g map[string]any) {
		g["description"] = plan.Description.ValueString()
		g["admin"] = plan.Admin.ValueBool()
		if !plan.LDAPGroupNames.IsNull() {
			g["ldapGroupNames"] = plan.LDAPGroupNames.ValueString()
		}
		for key, value := range permissions {
			g[key] = value
		}
	})
}

// readInto refreshes model from the instance. The permissions map is narrowed
// to the keys the configuration manages, so unmanaged abilities on the
// instance do not show up as drift.
func (r *groupResource) readInto(ctx context.Context, name string, model *groupResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	group, err := r.client.GetGroup(ctx, name)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku group",
			fmt.Sprintf("Reading group %q failed: %s", name, err),
		)
		return false, diags
	}

	managed := managedPermissionKeys(ctx, model.Permissions, &diags)
	if diags.HasError() {
		return false, diags
	}

	permissions := map[string]bool{}
	for _, key := range managed {
		permissions[key] = boolFromMap(group, key)
	}
	permissionsValue, d := types.MapValueFrom(ctx, types.BoolType, permissions)
	diags.Append(d...)
	if diags.HasError() {
		return false, diags
	}

	model.ID = types.StringValue(name)
	model.Name = types.StringValue(name)
	model.Description = nullIfEmpty(stringFromMap(group, "description"))
	model.SourceType = types.StringValue(stringFromMap(group, "sourceType"))
	model.Admin = types.BoolValue(boolFromMap(group, "admin"))
	model.LDAPGroupNames = nullIfEmpty(stringFromMap(group, "ldapGroupNames"))
	model.Permissions = permissionsValue
	return true, diags
}

// managedPermissionKeys returns the sorted keys currently tracked in the
// permissions map, or none when it is null or unknown.
func managedPermissionKeys(ctx context.Context, m types.Map, diags *diag.Diagnostics) []string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	current := map[string]bool{}
	diags.Append(m.ElementsAs(ctx, &current, false)...)
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
