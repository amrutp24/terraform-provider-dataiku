package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*projectPermissionsResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectPermissionsResource)(nil)
	_ resource.ResourceWithImportState = (*projectPermissionsResource)(nil)
)

// NewProjectPermissionsResource returns the dataiku_project_permissions resource.
func NewProjectPermissionsResource() resource.Resource { return &projectPermissionsResource{} }

type projectPermissionsResource struct {
	client *dataiku.Client
}

type projectPermissionsResourceModel struct {
	ID         types.String             `tfsdk:"id"`
	ProjectKey types.String             `tfsdk:"project_key"`
	Owner      types.String             `tfsdk:"owner"`
	Permission []projectPermissionModel `tfsdk:"permission"`
}

type projectPermissionModel struct {
	Group                          types.String `tfsdk:"group"`
	Admin                          types.Bool   `tfsdk:"admin"`
	ReadProjectContent             types.Bool   `tfsdk:"read_project_content"`
	WriteProjectContent            types.Bool   `tfsdk:"write_project_content"`
	ReadDashboards                 types.Bool   `tfsdk:"read_dashboards"`
	WriteDashboards                types.Bool   `tfsdk:"write_dashboards"`
	MoveJob                        types.Bool   `tfsdk:"move_job"`
	RunScenario                    types.Bool   `tfsdk:"run_scenario"`
	ExportDatasetsData             types.Bool   `tfsdk:"export_datasets_data"`
	ManageDashboardAuthorizations  types.Bool   `tfsdk:"manage_dashboard_authorizations"`
	ManageExposedElements          types.Bool   `tfsdk:"manage_exposed_elements"`
	ManageAdditionalDashboardUsers types.Bool   `tfsdk:"manage_additional_dashboard_users"`
	ShareToWorkspaces              types.Bool   `tfsdk:"share_to_workspaces"`
}

func (r *projectPermissionsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_permissions"
}

func permissionBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Optional:            true,
		Computed:            true,
		Default:             booldefault.StaticBool(false),
		MarkdownDescription: description,
	}
}

func (r *projectPermissionsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The complete access control list of a Dataiku DSS project.\n\n" +
			"This resource is authoritative: it replaces every group grant on the project, so a group " +
			"given access through the DSS interface is removed on the next apply. Use at most one " +
			"`dataiku_project_permissions` resource per project.\n\n" +
			"Destroying this resource clears all group grants but leaves the project and its owner in place.",
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
				MarkdownDescription: "Key of the project to manage permissions for. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"owner": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Login of the project owner. When omitted, the current owner on the " +
					"instance is kept. Do not set this if the project's owner is already managed by a " +
					"`dataiku_project` resource, or the two will fight.",
			},
		},
		Blocks: map[string]schema.Block{
			"permission": schema.ListNestedBlock{
				MarkdownDescription: "A set of rights granted to one group.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Name of the group these rights are granted to.",
						},
						"admin":                             permissionBool("Full administrative rights on the project."),
						"read_project_content":              permissionBool("Read datasets, recipes and other project content."),
						"write_project_content":             permissionBool("Create and modify project content."),
						"read_dashboards":                   permissionBool("View the project's dashboards."),
						"write_dashboards":                  permissionBool("Create and modify the project's dashboards."),
						"move_job":                          permissionBool("Start and abort jobs in the project."),
						"run_scenario":                      permissionBool("Run the project's scenarios."),
						"export_datasets_data":              permissionBool("Export the data of the project's datasets."),
						"manage_dashboard_authorizations":   permissionBool("Manage which objects dashboard readers may access."),
						"manage_exposed_elements":           permissionBool("Manage the objects the project exposes to other projects."),
						"manage_additional_dashboard_users": permissionBool("Manage additional dashboard-only users on the project."),
						"share_to_workspaces":               permissionBool("Share project objects to workspaces."),
					},
				},
			},
		},
	}
}

func (r *projectPermissionsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *projectPermissionsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectPermissionsResourceModel
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

func (r *projectPermissionsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectPermissionsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	perms, err := r.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		if dataiku.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
		)
		return
	}

	state.ID = types.StringValue(projectKey)
	state.Owner = types.StringValue(perms.Owner)
	state.Permission = flattenPermissions(perms.Permissions)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectPermissionsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectPermissionsResourceModel
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

func (r *projectPermissionsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectPermissionsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	current, err := r.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
		)
		return
	}

	// Clear the grants but keep the owner, which this resource does not own.
	current.Permissions = []dataiku.ProjectPermission{}
	if err := r.client.SetProjectPermissions(ctx, projectKey, *current); err != nil {
		resp.Diagnostics.AddError(
			"Unable to clear Dataiku project permissions",
			fmt.Sprintf("Clearing permissions of project %q failed: %s", projectKey, err),
		)
	}
}

func (r *projectPermissionsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("project_key"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// write pushes the planned ACL and reads back what the instance stored.
func (r *projectPermissionsResource) write(ctx context.Context, plan *projectPermissionsResourceModel, diags *diag.Diagnostics) {
	projectKey := plan.ProjectKey.ValueString()

	current, err := r.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		diags.AddError(
			"Unable to read Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
		)
		return
	}

	desired := dataiku.ProjectPermissions{
		Owner:       current.Owner,
		Permissions: expandPermissions(plan.Permission),
	}
	if !plan.Owner.IsNull() && !plan.Owner.IsUnknown() {
		desired.Owner = plan.Owner.ValueString()
	}

	if err := r.client.SetProjectPermissions(ctx, projectKey, desired); err != nil {
		diags.AddError(
			"Unable to set Dataiku project permissions",
			fmt.Sprintf("Setting permissions of project %q failed: %s", projectKey, err),
		)
		return
	}

	stored, err := r.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		diags.AddError(
			"Unable to read back Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q after writing them failed: %s", projectKey, err),
		)
		return
	}

	plan.ID = types.StringValue(projectKey)
	plan.Owner = types.StringValue(stored.Owner)
	plan.Permission = flattenPermissions(stored.Permissions)
}

func expandPermissions(blocks []projectPermissionModel) []dataiku.ProjectPermission {
	out := make([]dataiku.ProjectPermission, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, dataiku.ProjectPermission{
			Group:                          b.Group.ValueString(),
			Admin:                          b.Admin.ValueBool(),
			ReadProjectContent:             b.ReadProjectContent.ValueBool(),
			WriteProjectContent:            b.WriteProjectContent.ValueBool(),
			ReadDashboards:                 b.ReadDashboards.ValueBool(),
			WriteDashboards:                b.WriteDashboards.ValueBool(),
			MoveJob:                        b.MoveJob.ValueBool(),
			RunScenario:                    b.RunScenario.ValueBool(),
			ExportDatasetsData:             b.ExportDatasetsData.ValueBool(),
			ManageDashboardAuthorizations:  b.ManageDashboardAuthorizations.ValueBool(),
			ManageExposedElements:          b.ManageExposedElements.ValueBool(),
			ManageAdditionalDashboardUsers: b.ManageAdditionalDashboardUsers.ValueBool(),
			ShareToWorkspaces:              b.ShareToWorkspaces.ValueBool(),
		})
	}
	return out
}

func flattenPermissions(perms []dataiku.ProjectPermission) []projectPermissionModel {
	out := make([]projectPermissionModel, 0, len(perms))
	for _, p := range perms {
		// DSS also returns per-user grants on some instances; this resource
		// only models group grants, so skip anything without a group.
		if p.Group == "" {
			continue
		}
		out = append(out, projectPermissionModel{
			Group:                          types.StringValue(p.Group),
			Admin:                          types.BoolValue(p.Admin),
			ReadProjectContent:             types.BoolValue(p.ReadProjectContent),
			WriteProjectContent:            types.BoolValue(p.WriteProjectContent),
			ReadDashboards:                 types.BoolValue(p.ReadDashboards),
			WriteDashboards:                types.BoolValue(p.WriteDashboards),
			MoveJob:                        types.BoolValue(p.MoveJob),
			RunScenario:                    types.BoolValue(p.RunScenario),
			ExportDatasetsData:             types.BoolValue(p.ExportDatasetsData),
			ManageDashboardAuthorizations:  types.BoolValue(p.ManageDashboardAuthorizations),
			ManageExposedElements:          types.BoolValue(p.ManageExposedElements),
			ManageAdditionalDashboardUsers: types.BoolValue(p.ManageAdditionalDashboardUsers),
			ShareToWorkspaces:              types.BoolValue(p.ShareToWorkspaces),
		})
	}
	return out
}
