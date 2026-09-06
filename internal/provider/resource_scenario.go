package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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
	_ resource.Resource                = (*scenarioResource)(nil)
	_ resource.ResourceWithConfigure   = (*scenarioResource)(nil)
	_ resource.ResourceWithImportState = (*scenarioResource)(nil)
)

// NewScenarioResource returns the dataiku_scenario resource.
func NewScenarioResource() resource.Resource { return &scenarioResource{} }

type scenarioResource struct {
	client *dataiku.Client
}

type scenarioResourceModel struct {
	ID            types.String         `tfsdk:"id"`
	ScenarioID    types.String         `tfsdk:"scenario_id"`
	ProjectKey    types.String         `tfsdk:"project_key"`
	Name          types.String         `tfsdk:"name"`
	Type          types.String         `tfsdk:"type"`
	Active        types.Bool           `tfsdk:"active"`
	Tags          types.Set            `tfsdk:"tags"`
	TriggersJSON  jsontypes.Normalized `tfsdk:"triggers_json"`
	StepsJSON     jsontypes.Normalized `tfsdk:"steps_json"`
	ReportersJSON jsontypes.Normalized `tfsdk:"reporters_json"`
	Script        types.String         `tfsdk:"script"`
}

func (r *scenarioResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_scenario"
}

func (r *scenarioResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// 1: tags became a set. See UpgradeState below.
		Version: 1,
		MarkdownDescription: "A scenario in a Dataiku DSS project: what runs, and what makes it run.\n\n" +
			"Triggers, steps and reporters are supplied as JSON rather than as typed blocks. Their shape " +
			"varies by trigger and step type and between DSS versions, and DSS rewrites what you send — " +
			"it fills in defaults, renames some fields and stamps timestamps into others. Modelling them " +
			"as typed attributes would mean inventing names DSS may ignore, and refreshing them from the " +
			"instance would diff forever against those rewrites.\n\n" +
			"So this provider writes them and does not read them back. A change made in the DSS interface " +
			"to a scenario's triggers or steps will not appear as drift. On `terraform import` they are " +
			"read in once, which is also the easiest way to get a definition worth editing: build the " +
			"scenario in the interface, import it, and copy what comes out.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<project_key>/<scenario_id>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scenario_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier DSS gave the scenario. It is derived from the name rather " +
					"than random — `Nightly build` becomes `Nightly_build` — so two scenarios with the same " +
					"name in one project collide. It does not change when the scenario is renamed.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Key of the project the scenario belongs to. Changing this forces a new scenario.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the scenario. Renaming does not change `scenario_id`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("step_based"),
				MarkdownDescription: "`step_based` for a scenario built from steps, or `custom_python` for " +
					"one driven by a script. Changing this forces a new scenario.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("step_based", "custom_python"),
				},
			},
			"active": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the scenario's triggers are armed. A scenario with `active = false` only runs when started by hand.",
			},
			"tags": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Tags applied to the scenario.\n\n" +
					"A set, not a list: DSS stores tags unordered and hands them back in its own " +
					"order, so the order written here is not preserved and duplicates collapse.",
			},
			"triggers_json": schema.StringAttribute{
				Optional: true,
				// Computed as well as Optional: leaving it out of configuration is
				// allowed, and then whatever the instance has is adopted rather than
				// reported as an inconsistent result after apply.
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Triggers, as a JSON array. Written but never read back; see the note above.\n\n" +
					"```\ntriggers_json = jsonencode([{\n  id     = \"nightly\"\n  name   = \"nightly\"\n  type   = \"temporal\"\n  active = true\n  params = { repeatFrequency = 1, hour = 3, minute = 0, timezone = \"SERVER\" }\n}])\n```",
			},
			"steps_json": schema.StringAttribute{
				Optional: true,
				// Computed as well as Optional: leaving it out of configuration is
				// allowed, and then whatever the instance has is adopted rather than
				// reported as an inconsistent result after apply.
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Steps, as a JSON array, for a `step_based` scenario. This becomes " +
					"`params.steps` in the DSS definition. Written but never read back.",
			},
			"reporters_json": schema.StringAttribute{
				Optional: true,
				// Computed as well as Optional: leaving it out of configuration is
				// allowed, and then whatever the instance has is adopted rather than
				// reported as an inconsistent result after apply.
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Reporters, as a JSON array — what DSS notifies when the scenario " +
					"finishes. Written but never read back. May carry webhook URLs and credentials, so it is " +
					"treated as sensitive.",
				Sensitive: true,
			},
			"script": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Python source for a `custom_python` scenario. Ignored for `step_based`. " +
					"Unlike the JSON attributes this is stored verbatim by DSS, so it is read back and does " +
					"show drift.",
			},
		},
	}
}

func (r *scenarioResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *scenarioResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scenarioResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := plan.ProjectKey.ValueString()

	definition := map[string]any{
		"name":   plan.Name.ValueString(),
		"type":   plan.Type.ValueString(),
		"params": map[string]any{},
	}

	scenarioID, err := r.client.CreateScenario(ctx, projectKey, definition)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku scenario",
			fmt.Sprintf("Creating scenario %q in project %q failed: %s", plan.Name.ValueString(), projectKey, err),
		)
		return
	}
	if scenarioID == "" {
		resp.Diagnostics.AddError(
			"Dataiku did not return a scenario id",
			fmt.Sprintf("Creating scenario %q in project %q returned no id.", plan.Name.ValueString(), projectKey),
		)
		return
	}

	// Creation only registers the scenario; the triggers, steps and flags go on
	// with an update.
	if err := r.applyDefinition(ctx, projectKey, scenarioID, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Unable to configure Dataiku scenario",
			fmt.Sprintf("The scenario %q was created but applying its definition failed: %s", scenarioID, err),
		)
		return
	}

	resp.Diagnostics.Append(r.readInto(ctx, projectKey, scenarioID, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scenarioResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scenarioResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	scenarioID := state.ScenarioID.ValueString()

	if _, err := r.client.GetScenario(ctx, projectKey, scenarioID); err != nil {
		if dataiku.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read Dataiku scenario",
			fmt.Sprintf("Reading scenario %q failed: %s", scenarioID, err),
		)
		return
	}

	resp.Diagnostics.Append(r.readInto(ctx, projectKey, scenarioID, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scenarioResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state scenarioResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := plan.ProjectKey.ValueString()
	scenarioID := state.ScenarioID.ValueString()

	if err := r.applyDefinition(ctx, projectKey, scenarioID, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku scenario",
			fmt.Sprintf("Updating scenario %q failed: %s", scenarioID, err),
		)
		return
	}

	resp.Diagnostics.Append(r.readInto(ctx, projectKey, scenarioID, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scenarioResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scenarioResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := state.ProjectKey.ValueString()
	scenarioID := state.ScenarioID.ValueString()
	if err := r.client.DeleteScenario(ctx, projectKey, scenarioID); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku scenario",
			fmt.Sprintf("Deleting scenario %q failed: %s", scenarioID, err),
		)
	}
}

// ImportState accepts "<project_key>/<scenario_id>".
func (r *scenarioResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectKey, scenarioID, ok := strings.Cut(req.ID, "/")
	if !ok || projectKey == "" || scenarioID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a scenario as \"<project_key>/<scenario_id>\", for example \"ANALYTICS/Nightly_build\", got %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_key"), projectKey)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("scenario_id"), scenarioID)...)
}

// applyDefinition writes the managed fields onto the scenario's current
// definition and, for a custom_python scenario, its script.
func (r *scenarioResource) applyDefinition(ctx context.Context, projectKey, scenarioID string, plan *scenarioResourceModel) error {
	tags := []string{}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		if diags := plan.Tags.ElementsAs(ctx, &tags, false); diags.HasError() {
			return fmt.Errorf("reading tags: %v", diags.Errors())
		}
	}

	triggers, err := decodeJSONArray(plan.TriggersJSON, "triggers_json")
	if err != nil {
		return err
	}
	steps, err := decodeJSONArray(plan.StepsJSON, "steps_json")
	if err != nil {
		return err
	}
	reporters, err := decodeJSONArray(plan.ReportersJSON, "reporters_json")
	if err != nil {
		return err
	}

	updateErr := r.client.UpdateScenario(ctx, projectKey, scenarioID, func(s map[string]any) {
		s["name"] = plan.Name.ValueString()
		s["active"] = plan.Active.ValueBool()
		s["tags"] = tags

		if triggers != nil {
			s["triggers"] = triggers
		}
		if reporters != nil {
			s["reporters"] = reporters
		}
		if steps != nil {
			// Steps live under params, alongside settings this provider does
			// not model, so only the steps key is replaced.
			params, ok := s["params"].(map[string]any)
			if !ok {
				params = map[string]any{}
			}
			params["steps"] = steps
			s["params"] = params
		}
	})
	if updateErr != nil {
		return updateErr
	}

	if plan.Type.ValueString() == "custom_python" && !plan.Script.IsNull() && !plan.Script.IsUnknown() {
		if err := r.client.SetScenarioPayload(ctx, projectKey, scenarioID, plan.Script.ValueString()); err != nil {
			return fmt.Errorf("setting the scenario script: %w", err)
		}
	}
	return nil
}

// readInto refreshes the fields DSS stores verbatim. The JSON attributes are
// deliberately left as configured, because DSS rewrites them on write and
// reading them back would diff on every plan.
func (r *scenarioResource) readInto(ctx context.Context, projectKey, scenarioID string, model *scenarioResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	scenario, err := r.client.GetScenario(ctx, projectKey, scenarioID)
	if err != nil {
		diags.AddError(
			"Unable to read Dataiku scenario",
			fmt.Sprintf("Reading scenario %q failed: %s", scenarioID, err),
		)
		return diags
	}

	model.ID = types.StringValue(projectKey + "/" + scenarioID)
	model.ScenarioID = types.StringValue(scenarioID)
	model.ProjectKey = types.StringValue(projectKey)
	model.Name = types.StringValue(stringFromMap(scenario, "name"))
	model.Type = types.StringValue(stringFromMap(scenario, "type"))
	model.Active = types.BoolValue(boolFromMap(scenario, "active"))
	model.Tags = toStringSet(ctx, stringSliceFromMap(scenario, "tags"), &diags)

	// On import there is nothing configured to preserve, so adopt what the
	// instance has as a starting point.
	if model.TriggersJSON.IsNull() || model.TriggersJSON.IsUnknown() {
		model.TriggersJSON = encodeJSONArray(scenario["triggers"])
	}
	if model.ReportersJSON.IsNull() || model.ReportersJSON.IsUnknown() {
		model.ReportersJSON = encodeJSONArray(scenario["reporters"])
	}
	if model.StepsJSON.IsNull() || model.StepsJSON.IsUnknown() {
		if params, ok := scenario["params"].(map[string]any); ok {
			model.StepsJSON = encodeJSONArray(params["steps"])
		}
	}

	// script has to be resolved either way: it is computed, so leaving it
	// unknown after apply is an error even for a step_based scenario that has
	// no script at all.
	if model.Type.ValueString() == "custom_python" {
		script, err := r.client.GetScenarioPayload(ctx, projectKey, scenarioID)
		if err != nil {
			diags.AddError(
				"Unable to read Dataiku scenario script",
				fmt.Sprintf("Reading the script of scenario %q failed: %s", scenarioID, err),
			)
			return diags
		}
		model.Script = nullIfEmpty(script)
	} else {
		model.Script = types.StringNull()
	}
	return diags
}

// decodeJSONArray turns a configured JSON document into a slice, returning nil
// when the attribute is absent so the caller can leave the field untouched.
func decodeJSONArray(value jsontypes.Normalized, attribute string) ([]any, error) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	out := []any{}
	if err := json.Unmarshal([]byte(value.ValueString()), &out); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array: %w", attribute, err)
	}
	return out, nil
}

func encodeJSONArray(value any) jsontypes.Normalized {
	if value == nil {
		return jsontypes.NewNormalizedNull()
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(encoded))
}
