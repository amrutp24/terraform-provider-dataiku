package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*containerSettingsResource)(nil)
	_ resource.ResourceWithConfigure   = (*containerSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*containerSettingsResource)(nil)
)

// NewContainerSettingsResource returns dataiku_container_settings.
func NewContainerSettingsResource() resource.Resource {
	return &containerSettingsResource{}
}

// containerSettingsResource manages the instance-wide defaults for
// containerized execution: the "Default settings" block of Administration →
// Settings → Containerized execution.
//
// There is one of these per instance, so the resource is a singleton: it takes
// no name, and two of them in one configuration fight over the same fields.
type containerSettingsResource struct {
	client *dataiku.Client
}

type containerSettingsResourceModel struct {
	ID                              types.String `tfsdk:"id"`
	UseImplicitK8sCluster           types.Bool   `tfsdk:"use_implicit_k8s_cluster"`
	DefaultK8sClusterID             types.String `tfsdk:"default_k8s_cluster_id"`
	DefaultExecutionConfig          types.String `tfsdk:"default_execution_config"`
	DefaultExecutionConfigVisual    types.String `tfsdk:"default_execution_config_for_visual_recipes"`
	DefaultExecutionConfigExporters types.String `tfsdk:"default_execution_config_for_exporters"`
}

// containerSetting ties one Terraform argument to one field of the general
// settings document.
//
// A table rather than five copies of the same code: every field goes through
// the same write, verify, read and restore, and only the path and the type
// differ.
type containerSetting struct {
	attr string
	// path into the general settings document, outermost key first.
	path []string
	// wanted returns the value the plan asks for, or nil for "leave whatever
	// the instance has".
	wanted func(*containerSettingsResourceModel) any
	// record writes what the instance holds into the model. present is false
	// when the field is absent from the document, which is how DSS spells
	// "nothing selected".
	record func(*containerSettingsResourceModel, any, bool)
}

var containerSettings = []containerSetting{
	{
		attr: "use_implicit_k8s_cluster",
		path: []string{"useImplicitK8sCluster"},
		wanted: func(m *containerSettingsResourceModel) any {
			return boolOrNil(m.UseImplicitK8sCluster)
		},
		record: func(m *containerSettingsResourceModel, v any, present bool) {
			b, ok := v.(bool)
			m.UseImplicitK8sCluster = types.BoolValue(present && ok && b)
		},
	},
	{
		attr: "default_k8s_cluster_id",
		path: []string{"defaultK8sClusterId"},
		wanted: func(m *containerSettingsResourceModel) any {
			return stringOrNil(m.DefaultK8sClusterID)
		},
		record: func(m *containerSettingsResourceModel, v any, present bool) {
			m.DefaultK8sClusterID = optionalString(v, present)
		},
	},
	{
		attr: "default_execution_config",
		path: []string{"containerSettings", "defaultExecutionConfig"},
		wanted: func(m *containerSettingsResourceModel) any {
			return stringOrNil(m.DefaultExecutionConfig)
		},
		record: func(m *containerSettingsResourceModel, v any, present bool) {
			m.DefaultExecutionConfig = optionalString(v, present)
		},
	},
	{
		attr: "default_execution_config_for_visual_recipes",
		path: []string{"containerSettings", "defaultExecutionConfigForVisualRecipesWorkloads"},
		wanted: func(m *containerSettingsResourceModel) any {
			return stringOrNil(m.DefaultExecutionConfigVisual)
		},
		record: func(m *containerSettingsResourceModel, v any, present bool) {
			m.DefaultExecutionConfigVisual = optionalString(v, present)
		},
	},
	{
		attr: "default_execution_config_for_exporters",
		path: []string{"containerSettings", "defaultExecutionConfigForExporters"},
		wanted: func(m *containerSettingsResourceModel) any {
			return stringOrNil(m.DefaultExecutionConfigExporters)
		},
		record: func(m *containerSettingsResourceModel, v any, present bool) {
			m.DefaultExecutionConfigExporters = optionalString(v, present)
		},
	},
}

func boolOrNil(v types.Bool) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBool()
}

func stringOrNil(v types.String) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueString()
}

// optionalString keeps an absent field null rather than "", so that a setting
// nobody has chosen does not read back as a value.
func optionalString(v any, present bool) types.String {
	if !present {
		return types.StringNull()
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func (r *containerSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_settings"
}

func (r *containerSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The instance-wide defaults for containerized execution: which cluster DSS " +
			"runs containers on, and which execution configuration each kind of workload uses when the " +
			"project does not name one.\n\n" +
			"`use_implicit_k8s_cluster` is the one to reach for first. With no default cluster set and " +
			"that flag off, every containerized activity fails before it starts with **\"No default " +
			"Kubernetes cluster selected\"**, however correct the execution configuration is. Turning it " +
			"on tells DSS to use whatever cluster `kubectl` on the DSS host is pointed at, which is what " +
			"a `gcloud container clusters get-credentials` at boot leaves behind.\n\n" +
			"There is one of these per instance, so this is a singleton: it takes no name, and two " +
			"instances of it in one configuration overwrite each other. Destroying it puts the fields " +
			"back the way they were when Terraform first took them over.\n\n" +
			"Every argument left unset is left alone on the instance rather than cleared, and read back " +
			"into state. Setting an argument and later removing it from the configuration therefore " +
			"leaves the value in place; set it to the value you want instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Always `container_settings`. The instance has exactly one of these.",
			},
			"use_implicit_k8s_cluster": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Use the cluster `kubectl` on the DSS host defaults to, when no " +
					"`default_k8s_cluster_id` is set. This is \"Use builtin kubernetes cluster\" in the " +
					"DSS interface.",
			},
			"default_k8s_cluster_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Id of the DSS cluster object to use for projects that name no " +
					"cluster of their own. Applies to container execution and Spark on Kubernetes alike. " +
					"Leave unset to rely on `use_implicit_k8s_cluster`.",
			},
			"default_execution_config": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Name of the `dataiku_container_execution_config` to run user code " +
					"in — code recipes and notebooks — for projects that name none. Leave unset to run " +
					"user code on the DSS host.",
			},
			"default_execution_config_for_visual_recipes": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Name of the execution configuration for visual recipes. DSS " +
					"ignores this unless containerized execution of visual recipes is enabled on the " +
					"instance.",
			},
			"default_execution_config_for_exporters": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Name of the execution configuration for custom exporters that " +
					"can be containerized.",
			},
		},
	}
}

func (r *containerSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

// apply writes every field the plan sets, then reads the document back and
// checks that DSS kept them.
//
// The read-back is not defensive padding. DSS discards a top-level key it does
// not recognise while still answering 200 — a misspelled field name produces a
// successful apply and an instance that changed nothing — so the only way to
// report the truth is to look.
func (r *containerSettingsResource) apply(ctx context.Context, plan *containerSettingsResourceModel, diags *diag.Diagnostics) {
	err := r.client.UpdateGeneralSettings(ctx, func(settings map[string]any) error {
		for _, field := range containerSettings {
			if value := field.wanted(plan); value != nil {
				setPath(settings, field.path, value)
			}
		}
		return nil
	})
	if err != nil {
		diags.AddError(
			"Unable to write Dataiku containerized execution settings",
			fmt.Sprintf("Writing the instance's general settings failed: %s", err),
		)
		return
	}

	settings, err := r.client.GeneralSettings(ctx)
	if err != nil {
		diags.AddError(
			"Unable to read back Dataiku containerized execution settings",
			fmt.Sprintf("The write succeeded but reading the settings back failed: %s", err),
		)
		return
	}

	var dropped []string
	for _, field := range containerSettings {
		want := field.wanted(plan)
		if want == nil {
			continue
		}
		got, present := getPath(settings, field.path)
		if !present {
			dropped = append(dropped, fmt.Sprintf("%s (dropped)", field.attr))
			continue
		}
		if !jsonEqualValues(want, got) {
			dropped = append(dropped, fmt.Sprintf("%s (sent %v, stored %v)", field.attr, want, got))
		}
	}
	if len(dropped) > 0 {
		diags.AddError(
			"Dataiku did not apply every containerized execution setting",
			fmt.Sprintf("DSS answered success but did not store these settings as sent:\n\n  %s\n\n"+
				"DSS discards fields it does not recognise rather than rejecting them, so this usually "+
				"means the field has moved or been renamed in this DSS version.",
				strings.Join(dropped, "\n  ")),
		)
		return
	}

	r.recordInto(settings, plan)
}

// recordInto fills the model from the document, for the fields the plan left
// alone as well as the ones it set.
func (r *containerSettingsResource) recordInto(settings map[string]any, model *containerSettingsResourceModel) {
	model.ID = types.StringValue("container_settings")
	for _, field := range containerSettings {
		value, present := getPath(settings, field.path)
		field.record(model, value, present)
	}
}

// priorState is what the fields held before Terraform first wrote them, kept in
// private state so that a destroy can put them back. A field that was absent is
// recorded as absent and removed again rather than set to a zero value.
type priorState struct {
	Values map[string]json.RawMessage `json:"values"`
}

const containerSettingsPriorKey = "prior"

func capturePrior(settings map[string]any) ([]byte, error) {
	prior := priorState{Values: map[string]json.RawMessage{}}
	for _, field := range containerSettings {
		value, present := getPath(settings, field.path)
		if !present {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		prior.Values[field.attr] = encoded
	}
	return json.Marshal(prior)
}

func (r *containerSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read before writing, so that a destroy can restore what was here. There
	// is nothing to create on the instance: these fields already exist, and
	// this resource takes over the ones it is given.
	before, err := r.client.GeneralSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku containerized execution settings",
			err.Error(),
		)
		return
	}
	encoded, err := capturePrior(before)
	if err != nil {
		resp.Diagnostics.AddError("Unable to record the previous settings", err.Error())
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Private.SetKey(ctx, containerSettingsPriorKey, encoded)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerSettingsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, err := r.client.GeneralSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Dataiku containerized execution settings", err.Error())
		return
	}

	r.recordInto(settings, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *containerSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan containerSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Nothing re-records what the fields held before Terraform took them over.
	// That capture belongs to the first write, and the framework hands the same
	// private data to the request and the response of an update, so it survives
	// on its own; copying it here would be a no-op.
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete puts the fields back as they were before Terraform took them over.
//
// Nothing is deleted on the instance, because nothing was created: these
// settings exist whether or not anything manages them. Leaving them at
// Terraform's values would be the surprising choice, so a field that was absent
// is removed again and a field that had a value gets it back.
func (r *containerSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	encoded, diags := req.Private.GetKey(ctx, containerSettingsPriorKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if encoded == nil {
		// An imported resource has no capture: Terraform never saw the values
		// this replaced, so there is nothing to put back.
		resp.Diagnostics.AddWarning(
			"Left the containerized execution settings as they are",
			"This resource was imported rather than created, so the provider never recorded what "+
				"these settings held beforehand and cannot restore them. The values are unchanged on "+
				"the instance; set them under Administration → Settings → Containerized execution if "+
				"that is not what you want.",
		)
		return
	}

	var prior priorState
	if err := json.Unmarshal(encoded, &prior); err != nil {
		resp.Diagnostics.AddError("Unable to read the recorded previous settings", err.Error())
		return
	}

	err := r.client.UpdateGeneralSettings(ctx, func(settings map[string]any) error {
		for _, field := range containerSettings {
			raw, had := prior.Values[field.attr]
			if !had {
				deletePath(settings, field.path)
				continue
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			setPath(settings, field.path, value)
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to restore Dataiku containerized execution settings",
			fmt.Sprintf("Putting the settings back as they were failed: %s", err),
		)
	}
}

// ImportState adopts whatever the instance holds. The id is ignored: there is
// only one of these, so the address is the whole of the import.
func (r *containerSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "container_settings")...)
}
