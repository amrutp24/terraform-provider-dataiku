package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = (*projectFolderResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectFolderResource)(nil)
	_ resource.ResourceWithImportState = (*projectFolderResource)(nil)
)

// NewProjectFolderResource returns the dataiku_project_folder resource.
func NewProjectFolderResource() resource.Resource { return &projectFolderResource{} }

type projectFolderResource struct {
	client *dataiku.Client
}

type projectFolderResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	ParentID types.String `tfsdk:"parent_id"`
	Owner    types.String `tfsdk:"owner"`
}

func (r *projectFolderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_folder"
}

func (r *projectFolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A folder that projects are organised into on a Dataiku DSS instance.\n\n" +
			"Use this with `dataiku_project`'s `project_folder_id` to place projects into a hierarchy. " +
			"DSS assigns the id, so reference it as `dataiku_project_folder.<name>.id` rather than " +
			"writing one out.\n\n" +
			"DSS refuses to delete a folder that still holds projects or child folders, so destroying " +
			"this resource fails until whatever it contains has moved or gone.\n\n" +
			"What a folder contains is deliberately not exposed here. Those are reverse references — " +
			"projects and child folders point at the folder rather than the other way round — so a " +
			"resource attribute would always be read before the things that populate it exist. Use the " +
			"`dataiku_project_folder` data source to see a folder's contents.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier DSS assigned to the folder.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the folder.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"parent_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(dataiku.RootProjectFolderID),
				MarkdownDescription: "Id of the folder this one sits in. Defaults to `ROOT`, the top of the " +
					"hierarchy. Changing this moves the folder rather than recreating it.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"owner": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Login that owns the folder. DSS sets this to whoever created it.",
			},
		},
	}
}

func (r *projectFolderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *projectFolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectFolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parentID := plan.ParentID.ValueString()
	name := plan.Name.ValueString()

	created, err := r.client.CreateProjectFolder(ctx, parentID, name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku project folder",
			fmt.Sprintf("Creating folder %q under %q failed: %s", name, parentID, err),
		)
		return
	}

	found, diags := r.readInto(ctx, created.ID, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku project folder disappeared after creation",
			fmt.Sprintf("The folder %q was created but could not be read back.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFolderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectFolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readInto(ctx, state.ID.ValueString(), &state)
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

func (r *projectFolderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectFolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// A rename goes through the settings document; reparenting is a separate
	// call, so a change to both means two requests.
	if !plan.Name.Equal(state.Name) {
		err := r.client.UpdateProjectFolderSettings(ctx, id, func(s map[string]any) {
			s["name"] = plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to rename Dataiku project folder",
				fmt.Sprintf("Renaming folder %q failed: %s", id, err),
			)
			return
		}
	}

	if !plan.ParentID.Equal(state.ParentID) {
		if err := r.client.MoveProjectFolder(ctx, id, plan.ParentID.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				"Unable to move Dataiku project folder",
				fmt.Sprintf("Moving folder %q into %q failed: %s", id, plan.ParentID.ValueString(), err),
			)
			return
		}
	}

	found, diags := r.readInto(ctx, id, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku project folder disappeared during update",
			fmt.Sprintf("The folder %q could not be read back after being updated.", id),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFolderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectFolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteProjectFolder(ctx, id); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku project folder",
			fmt.Sprintf("Deleting folder %q failed: %s\n\n"+
				"DSS refuses to delete a folder that still holds projects or child folders.", id, err),
		)
	}
}

func (r *projectFolderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *projectFolderResource) readInto(ctx context.Context, id string, model *projectFolderResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	folder, err := r.client.GetProjectFolder(ctx, id)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku project folder",
			fmt.Sprintf("Reading folder %q failed: %s", id, err),
		)
		return false, diags
	}

	// The folder payload does not carry the owner, so it comes from settings.
	owner := ""
	if settings, err := r.client.GetProjectFolderSettings(ctx, id); err == nil {
		owner = stringFromMap(settings, "owner")
	}

	model.ID = types.StringValue(folder.ID)
	model.Name = types.StringValue(folder.Name)
	model.Owner = types.StringValue(owner)

	// The root folder reports no parent.
	if folder.ParentID == "" {
		model.ParentID = types.StringValue(dataiku.RootProjectFolderID)
	} else {
		model.ParentID = types.StringValue(folder.ParentID)
	}
	return true, diags
}
