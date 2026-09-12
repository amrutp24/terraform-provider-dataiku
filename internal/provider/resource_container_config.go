package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

// containerConfigResource backs both dataiku_container_image_build_config and
// dataiku_container_execution_config. The two are entries in different lists of
// the same document and behave identically, so they share everything but their
// type name, their list, and their documentation.
type containerConfigResource struct {
	client *dataiku.Client

	kind         dataiku.ContainerConfigKind
	typeSuffix   string
	description  string
	settingsDesc string
}

type containerConfigResourceModel struct {
	ID            types.String         `tfsdk:"id"`
	Name          types.String         `tfsdk:"name"`
	SettingsJSON  jsontypes.Normalized `tfsdk:"settings_json"`
	EffectiveJSON jsontypes.Normalized `tfsdk:"effective_json"`
}

func (r *containerConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.typeSuffix
}

func (r *containerConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: r.description + "\n\n" +
			"**Dataiku documents no field of this object.** Neither the reference documentation " +
			"nor the REST API reference names a single key of `containerSettings`, and the REST " +
			"reference says only that you must PUT back an object you previously fetched. The " +
			"field names below were recovered by reading a running DSS 15 instance, and the " +
			"structure has already changed between DSS majors, so pin your DSS version and " +
			"re-check after an upgrade.\n\n" +
			"This resource edits two lists inside the instance-wide general settings document. " +
			"Anything the provider does not manage in that document is read and written back " +
			"untouched, and writes are serialised so that resources applied in parallel do not " +
			"overwrite one another.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The configuration name. Same value as `name`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Name of the configuration, unique on the instance. Changing this " +
					"forces a new configuration.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"settings_json": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: r.settingsDesc + "\n\n" +
					"Supplied as JSON rather than as typed arguments because DSS rewrites what it " +
					"is given: it fills in defaults, moves values into nested runtime blocks, and " +
					"**silently discards anything it does not recognise at the level it was " +
					"supplied, while still answering 200**. Typed arguments would report success " +
					"for settings the instance is not running.\n\n" +
					"Every key set here is read back after the write and compared. A key DSS drops " +
					"or changes fails the apply and names itself, rather than passing quietly.\n\n" +
					"`name` is managed by the `name` argument and must not appear here.",
			},
			"effective_json": schema.StringAttribute{
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "The whole entry as DSS stores it, including everything it added. " +
					"Useful for discovering the fields this instance's version actually supports: set " +
					"a configuration in the DSS interface, import it, and read this.",
			},
		},
	}
}

func (r *containerConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

// wanted decodes settings_json into the object to send.
func (r *containerConfigResource) wanted(plan *containerConfigResourceModel, diags *diag.Diagnostics) map[string]any {
	entry := map[string]any{}
	if err := json.Unmarshal([]byte(plan.SettingsJSON.ValueString()), &entry); err != nil {
		diags.AddAttributeError(
			path.Root("settings_json"),
			"Invalid settings JSON",
			fmt.Sprintf("settings_json must be a JSON object: %s", err),
		)
		return nil
	}
	if _, present := entry["name"]; present {
		diags.AddAttributeError(
			path.Root("settings_json"),
			"name must not appear in settings_json",
			"The configuration's name is managed by the `name` argument. Remove \"name\" from settings_json.",
		)
		return nil
	}
	return entry
}

// write sends the entry and then checks that DSS kept what was asked for.
func (r *containerConfigResource) write(ctx context.Context, plan *containerConfigResourceModel, diags *diag.Diagnostics) {
	name := plan.Name.ValueString()

	entry := r.wanted(plan, diags)
	if diags.HasError() {
		return
	}

	// PutContainerConfig sets name on the copy it sends, so compare without it.
	sent := make(map[string]any, len(entry))
	maps.Copy(sent, entry)

	if err := r.client.PutContainerConfig(ctx, r.kind, name, entry); err != nil {
		diags.AddError(
			"Unable to write Dataiku container configuration",
			fmt.Sprintf("Writing %s %q failed: %s", r.typeSuffix, name, err),
		)
		return
	}

	stored, err := r.client.FindContainerConfig(ctx, r.kind, name)
	if err != nil {
		diags.AddError(
			"Unable to read back Dataiku container configuration",
			fmt.Sprintf("The write of %s %q succeeded but reading it back failed: %s", r.typeSuffix, name, err),
		)
		return
	}
	if stored == nil {
		diags.AddError(
			"Dataiku accepted the container configuration and did not store it",
			fmt.Sprintf("DSS answered success for %s %q, but the configuration is absent when read back. "+
				"This is how DSS reports a configuration it rejected for a reason it does not surface.",
				r.typeSuffix, name),
		)
		return
	}

	if dropped := dataiku.VerifyContainerConfigApplied(sent, stored); len(dropped) > 0 {
		diags.AddError(
			"Dataiku did not apply every setting",
			fmt.Sprintf("DSS answered success for %s %q but did not store these settings as sent:\n\n  %s\n\n"+
				"DSS discards fields it does not recognise at the level they were supplied rather than "+
				"rejecting them, so this usually means a key belongs inside a nested block. Import an "+
				"equivalent configuration made in the DSS interface and read `effective_json` to see "+
				"where this version expects it.",
				r.typeSuffix, name, strings.Join(dropped, "\n  ")),
		)
		return
	}

	r.intoModel(stored, plan, diags)
}

// intoModel records what DSS stored.
func (r *containerConfigResource) intoModel(stored map[string]any, model *containerConfigResourceModel, diags *diag.Diagnostics) {
	encoded, err := json.Marshal(stored)
	if err != nil {
		diags.AddError("Unable to encode container configuration", err.Error())
		return
	}
	model.ID = types.StringValue(model.Name.ValueString())
	model.EffectiveJSON = jsontypes.NewNormalizedValue(string(encoded))
}

func (r *containerConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.client.FindContainerConfig(ctx, r.kind, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku container configurations",
			err.Error(),
		)
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError(
			"Container configuration already exists",
			fmt.Sprintf("A %s named %q already exists on the instance. Import it with "+
				"`terraform import` rather than creating it, or choose another name.",
				r.typeSuffix, plan.Name.ValueString()),
		)
		return
	}

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		// The write itself may well have landed: DSS answers 200 and only then
		// does the read-back show it did not keep what was asked for. Nothing
		// is in state, so without this the entry is orphaned on the instance,
		// and an orphaned execution config referencing a build config blocks
		// every later write of the whole settings document.
		if err := r.client.DeleteContainerConfig(ctx, r.kind, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddWarning(
				"Left a partial container configuration on the instance",
				fmt.Sprintf("After the failure above, removing %s %q did not succeed either: %s\n\n"+
					"Delete it under Administration → Settings → Containerized execution before "+
					"applying again.", r.typeSuffix, plan.Name.ValueString(), err),
			)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stored, err := r.client.FindContainerConfig(ctx, r.kind, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Dataiku container configuration", err.Error())
		return
	}
	if stored == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	r.intoModel(stored, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *containerConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan containerConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteContainerConfig(ctx, r.kind, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku container configuration",
			fmt.Sprintf("Deleting %s %q failed: %s", r.typeSuffix, state.Name.ValueString(), err),
		)
	}
}

func (r *containerConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	// settings_json is the practitioner's intent and cannot be recovered from
	// the instance, which holds it merged with everything DSS added. Seed it
	// empty so the first plan shows what the configuration would set, and read
	// effective_json to see what is actually there.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("settings_json"), "{}")...)
}
