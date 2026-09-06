package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

// projectKeyPattern mirrors what DSS accepts for a project key. Validating it
// here turns a confusing 500 from the API into a plan-time error.
var projectKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)

// NewProjectResource returns the dataiku_project resource.
func NewProjectResource() resource.Resource { return &projectResource{} }

type projectResource struct {
	client *dataiku.Client
}

type projectResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectKey      types.String `tfsdk:"project_key"`
	Name            types.String `tfsdk:"name"`
	Owner           types.String `tfsdk:"owner"`
	Description     types.String `tfsdk:"description"`
	ShortDesc       types.String `tfsdk:"short_desc"`
	Tags            types.Set    `tfsdk:"tags"`
	ProjectFolderID types.String `tfsdk:"project_folder_id"`

	ClearManagedDatasetsOnDelete      types.Bool `tfsdk:"clear_managed_datasets_on_delete"`
	ClearOutputManagedFoldersOnDelete types.Bool `tfsdk:"clear_output_managed_folders_on_delete"`
	ClearJobAndScenarioLogsOnDelete   types.Bool `tfsdk:"clear_job_and_scenario_logs_on_delete"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// 1: tags became a set. See UpgradeState below.
		Version: 1,
		MarkdownDescription: "A Dataiku DSS project.\n\n" +
			"Deleting this resource permanently deletes the project on the instance. " +
			"Use the `clear_*_on_delete` arguments to control what data DSS removes along with it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The project key. Same value as `project_key`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier of the project on the instance. Letters, digits and " +
					"underscores only. Changing this forces a new project.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(projectKeyPattern,
						"must contain only letters, digits and underscores"),
					stringvalidator.LengthAtLeast(1),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the project.",
			},
			"owner": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Login of the DSS user who owns the project.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Long description of the project, shown on the project home page.",
			},
			"short_desc": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Short description of the project, shown on the project tile.",
			},
			"tags": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Tags applied to the project.\n\n" +
					"A set, not a list: DSS stores tags unordered and hands them back in its own " +
					"order, so the order written here is not preserved and duplicates collapse.",
			},
			"project_folder_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the project folder to create the project in. Defaults to the root " +
					"folder. DSS does not report a project's folder through the public API, so this value is " +
					"only applied at creation and is not refreshed. Changing it forces a new project.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"clear_managed_datasets_on_delete": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether to clear the data of managed datasets when the project is deleted.",
			},
			"clear_output_managed_folders_on_delete": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether to clear the data of managed folders used as recipe outputs when the project is deleted.",
			},
			"clear_job_and_scenario_logs_on_delete": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether to clear job and scenario logs when the project is deleted.",
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := plan.ProjectKey.ValueString()

	createReq := dataiku.CreateProjectRequest{
		ProjectKey:      projectKey,
		Name:            plan.Name.ValueString(),
		Owner:           plan.Owner.ValueString(),
		Description:     plan.Description.ValueString(),
		Tags:            fromStringSet(ctx, plan.Tags, &resp.Diagnostics),
		ProjectFolderID: plan.ProjectFolderID.ValueString(),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.CreateProject(ctx, createReq); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku project",
			fmt.Sprintf("Creating project %q failed: %s", projectKey, err),
		)
		return
	}

	// short_desc is not part of the creation payload, so apply it (and
	// re-assert name/description) through the metadata endpoint.
	if !plan.ShortDesc.IsNull() {
		err := r.client.UpdateProjectMetadata(ctx, projectKey, func(md map[string]any) {
			md["shortDesc"] = plan.ShortDesc.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to set project metadata",
				fmt.Sprintf("The project %q was created but setting short_desc failed: %s", projectKey, err),
			)
			return
		}
	}

	plan.ID = types.StringValue(projectKey)
	_, diags := r.readInto(ctx, projectKey, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	found, diags := r.readInto(ctx, projectKey, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// The project was deleted outside Terraform; drop it from state so
		// the next plan recreates it instead of failing.
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := plan.ProjectKey.ValueString()
	tags := fromStringSet(ctx, plan.Tags, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateProjectMetadata(ctx, projectKey, func(md map[string]any) {
		md["label"] = plan.Name.ValueString()
		md["description"] = plan.Description.ValueString()
		md["shortDesc"] = plan.ShortDesc.ValueString()
		md["tags"] = tags
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku project",
			fmt.Sprintf("Updating metadata of project %q failed: %s", projectKey, err),
		)
		return
	}

	// The owner lives on the permissions document, not the metadata one.
	if !plan.Owner.Equal(state.Owner) {
		perms, err := r.client.GetProjectPermissions(ctx, projectKey)
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to read Dataiku project permissions",
				fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
			)
			return
		}
		perms.Owner = plan.Owner.ValueString()
		if err := r.client.SetProjectPermissions(ctx, projectKey, *perms); err != nil {
			resp.Diagnostics.AddError(
				"Unable to change Dataiku project owner",
				fmt.Sprintf("Setting the owner of project %q to %q failed: %s", projectKey, perms.Owner, err),
			)
			return
		}
	}

	plan.ID = types.StringValue(projectKey)
	_, diags := r.readInto(ctx, projectKey, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	err := r.client.DeleteProject(ctx, projectKey, dataiku.DeleteProjectOptions{
		ClearManagedDatasets:      state.ClearManagedDatasetsOnDelete.ValueBool(),
		ClearOutputManagedFolders: state.ClearOutputManagedFoldersOnDelete.ValueBool(),
		ClearJobAndScenarioLogs:   state.ClearJobAndScenarioLogsOnDelete.ValueBool(),
	})
	if err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku project",
			fmt.Sprintf("Deleting project %q failed: %s", projectKey, err),
		)
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("project_key"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// readInto refreshes model from the instance. It reports false when the
// project no longer exists. project_folder_id is left untouched because DSS
// does not report a project's folder through the public API.
func (r *projectResource) readInto(ctx context.Context, projectKey string, model *projectResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	metadata, err := r.client.GetProjectMetadata(ctx, projectKey)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku project",
			fmt.Sprintf("Reading metadata of project %q failed: %s", projectKey, err),
		)
		return false, diags
	}

	perms, err := r.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		diags.AddError(
			"Unable to read Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
		)
		return false, diags
	}

	model.ID = types.StringValue(projectKey)
	model.ProjectKey = types.StringValue(projectKey)
	model.Name = types.StringValue(stringFromMap(metadata, "label"))
	model.Owner = types.StringValue(perms.Owner)
	model.Description = nullIfEmpty(stringFromMap(metadata, "description"))
	model.ShortDesc = nullIfEmpty(stringFromMap(metadata, "shortDesc"))
	model.Tags = toStringSet(ctx, stringSliceFromMap(metadata, "tags"), &diags)
	return true, diags
}
