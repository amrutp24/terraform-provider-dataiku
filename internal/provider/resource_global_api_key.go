package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                     = (*globalAPIKeyResource)(nil)
	_ resource.ResourceWithConfigure        = (*globalAPIKeyResource)(nil)
	_ resource.ResourceWithImportState      = (*globalAPIKeyResource)(nil)
	_ resource.ResourceWithConfigValidators = (*globalAPIKeyResource)(nil)
)

// NewGlobalAPIKeyResource returns the dataiku_global_api_key resource.
func NewGlobalAPIKeyResource() resource.Resource { return &globalAPIKeyResource{} }

type globalAPIKeyResource struct {
	client *dataiku.Client
}

type globalAPIKeyResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Label                types.String `tfsdk:"label"`
	Description          types.String `tfsdk:"description"`
	Admin                types.Bool   `tfsdk:"admin"`
	Permissions          types.Map    `tfsdk:"permissions"`
	Groups               types.Set    `tfsdk:"groups"`
	UserForImpersonation types.String `tfsdk:"user_for_impersonation"`
	LifetimeDays         types.Int64  `tfsdk:"lifetime_days"`
	ExpiresOn            types.String `tfsdk:"expires_on"`
	Key                  types.String `tfsdk:"key"`
}

func (r *globalAPIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_global_api_key"
}

func (r *globalAPIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A global API key on a Dataiku DSS instance. Requires an API key with admin rights.\n\n" +
			"DSS shows a key's secret once, when the key is created. This resource keeps it in `key`, " +
			"which means **the secret is stored in Terraform state**. Protect the state accordingly. " +
			"A key that was imported has no `key`, because DSS will not give the secret out again.\n\n" +
			"A key takes its rights in one of two ways, chosen with `groups`. Without `groups` the key " +
			"carries its own rights, set with `admin` and `permissions`. With `groups` it has whatever " +
			"those groups have and none of its own.\n\n" +
			"The provider still needs a key to start with. Create that first one by hand or with " +
			"`dsscli api-key-create`, then use this resource for every key after it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The identifier DSS gives the key. This is not the secret.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"label": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "A short name for the key, shown in the DSS key list. DSS does not " +
					"require labels to be unique.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "What the key is for.",
			},
			"admin": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the key has DSS administrator rights. Cannot be set together with `groups`.",
			},
			"permissions": schema.MapAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.BoolType,
				MarkdownDescription: "Global abilities granted to the key, keyed by the raw DSS field name. " +
					"The names are the ones `dataiku_group` takes in its own `permissions`, for example " +
					"`mayCreateProjects` or `mayWriteUnsafeCode`. Cannot be set together with `groups`.\n\n" +
					"A new key is not empty-handed. DSS 15 turns these on by default: `mayCreateProjects`, " +
					"`mayWriteInRootProjectFolder`, `mayCreateActiveWebContent`, `mayCreateDataCollections`, " +
					"`mayPublishToDataCollections` and `mayShareToWorkspaces`. Set them to `false` here for a " +
					"key that should not have them.\n\n" +
					"Abilities absent from this map are left at whatever the instance has. A name the " +
					"instance does not know is refused, because DSS would otherwise drop it and report success.",
			},
			"groups": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Names of the groups the key takes its rights from. Every group must " +
					"exist. Setting this, even to an empty set, makes the key group-based: it has no " +
					"`admin` flag and no `permissions` of its own.\n\n" +
					"Adding `groups` to an existing key changes it in place and keeps its secret. Removing " +
					"`groups` altogether forces a new key with a new secret, because DSS refuses to turn a " +
					"group-based key back into one with its own rights.",
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.SetRequest, resp *setplanmodifier.RequiresReplaceIfFuncResponse) {
							resp.RequiresReplace = !req.StateValue.IsNull() && req.PlanValue.IsNull()
						},
						"Removing groups from a group-based key forces a new key.",
						"Removing `groups` from a group-based key forces a new key.",
					),
				},
			},
			"user_for_impersonation": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Login of the DSS user associated with the key, the field DSS calls " +
					"`dssUserForImpersonation`. DSS stores whatever is given here without checking that " +
					"the user exists.",
			},
			"lifetime_days": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(0),
				MarkdownDescription: "How many days the key lasts. `0`, the default, means it does not " +
					"expire.\n\n" +
					"DSS counts from when the key was created, not from when this was last changed, so " +
					"setting `7` on a key made a month ago gives an expiry date in the past.",
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"expires_on": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "When the key expires, as an RFC 3339 timestamp in UTC. Null when " +
					"the key does not expire.",
			},
			"key": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "The secret, as DSS returned it when the key was created. Stored in " +
					"state. Null for an imported key.\n\n" +
					"The secret never changes while the key exists. To rotate it, replace the resource: " +
					"`terraform apply -replace=dataiku_global_api_key.NAME`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *globalAPIKeyResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(path.MatchRoot("groups"), path.MatchRoot("admin")),
		resourcevalidator.Conflicting(path.MatchRoot("groups"), path.MatchRoot("permissions")),
	}
}

func (r *globalAPIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

// apiKeySettings is the plan, read out of the framework types once so the
// create and update paths cannot disagree about what was asked for.
type apiKeySettings struct {
	label, description, impersonation string
	admin                             bool
	permissions                       map[string]bool
	groupBased                        bool
	groups                            []string
	lifetimeDays                      int64
}

func (r *globalAPIKeyResource) settings(ctx context.Context, plan *globalAPIKeyResourceModel, diags *diag.Diagnostics) apiKeySettings {
	s := apiKeySettings{
		label:         plan.Label.ValueString(),
		description:   plan.Description.ValueString(),
		impersonation: plan.UserForImpersonation.ValueString(),
		admin:         plan.Admin.ValueBool(),
		permissions:   map[string]bool{},
		groupBased:    !plan.Groups.IsNull() && !plan.Groups.IsUnknown(),
		lifetimeDays:  plan.LifetimeDays.ValueInt64(),
	}
	if !plan.Permissions.IsNull() && !plan.Permissions.IsUnknown() {
		diags.Append(plan.Permissions.ElementsAs(ctx, &s.permissions, false)...)
	}
	if s.groupBased {
		s.groups = fromStringSet(ctx, plan.Groups, diags)
		if s.groups == nil {
			s.groups = []string{}
		}
		sort.Strings(s.groups)
	}
	return s
}

// setOrDelete writes a string field, or removes it when the value is empty.
// DSS stores an empty string as an empty string, and an empty
// dssUserForImpersonation is not the same thing as none.
func setOrDelete(doc map[string]any, key, value string) {
	if value == "" {
		delete(doc, key)
		return
	}
	doc[key] = value
}

// apply writes the settings onto a key document, whether that is the body of
// a create or the current definition about to be written back.
func (s apiKeySettings) apply(doc map[string]any) {
	setOrDelete(doc, "label", s.label)
	setOrDelete(doc, "description", s.description)
	setOrDelete(doc, "dssUserForImpersonation", s.impersonation)
	doc["expiry"] = s.lifetimeDays

	if s.groupBased {
		// The presence of "groups" is what makes a key group-based. DSS drops
		// globalPermissions from such a key whether or not it is sent.
		doc["groups"] = s.groups
		delete(doc, "globalPermissions")
		return
	}

	global, _ := doc["globalPermissions"].(map[string]any)
	if global == nil {
		global = map[string]any{}
	}
	global["admin"] = s.admin
	for name, value := range s.permissions {
		global[name] = value
	}
	doc["globalPermissions"] = global
}

// unknownAbilities returns the abilities that were asked for and are missing
// from what DSS stored. DSS drops a name it does not know and answers 200, so
// reading the result back is the only way to notice.
func (s apiKeySettings) unknownAbilities(doc map[string]any) []string {
	if s.groupBased {
		return nil
	}
	global, _ := doc["globalPermissions"].(map[string]any)
	var missing []string
	for name := range s.permissions {
		if _, ok := global[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func unknownAbilitiesDetail(names []string) string {
	return fmt.Sprintf(
		"This instance does not know %s. DSS accepts an ability it does not recognise, answers 200 and "+
			"stores nothing, so the key would not have it. Check the spelling against an existing group: "+
			"the `dataiku_group` data source lists every ability the instance supports in `definition_json`.",
		"`"+strings.Join(names, "`, `")+"`")
}

func (r *globalAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan globalAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings := r.settings(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{}
	settings.apply(body)

	created, err := r.client.CreateGlobalAPIKey(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Dataiku global API key", fmt.Sprintf("Creating the key failed: %s", err))
		return
	}
	id := stringFromMap(created, "id")
	if id == "" {
		resp.Diagnostics.AddError(
			"Dataiku did not return an id for the new global API key",
			"The create call succeeded but the response carried no id, so the key cannot be tracked. "+
				"Look for it under Administration, Security, Global API keys.",
		)
		return
	}

	if missing := settings.unknownAbilities(created); len(missing) > 0 {
		// Nothing is in state yet, so take the key away again rather than
		// leave one behind that Terraform does not know about.
		detail := unknownAbilitiesDetail(missing)
		if err := r.client.DeleteGlobalAPIKey(ctx, id); err != nil && !dataiku.IsNotFound(err) {
			detail += fmt.Sprintf("\n\nThe key was created before this was noticed and could not be removed: %s. "+
				"Delete the key with id %s by hand.", err, id)
		}
		resp.Diagnostics.AddError("Unknown ability in permissions", detail)
		return
	}

	// Save what the create call returned before doing anything else. It is
	// the only response that will ever carry the secret.
	plan.Key = types.StringNull()
	if secret := stringFromMap(created, "key"); secret != "" && strings.Trim(secret, "*") != "" {
		plan.Key = types.StringValue(secret)
	} else {
		resp.Diagnostics.AddWarning(
			"Dataiku did not return the secret for the new global API key",
			"The key was created, but the response did not carry its secret, so `key` is null. "+
				"DSS will not show it again. Replace the resource to get a key whose secret is known.",
		)
	}
	resp.Diagnostics.Append(r.fill(ctx, created, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create response reports expiresOn as 0 even for a key with a
	// lifetime. A read straight after has the real date.
	found, diags := r.readInto(ctx, id, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku global API key disappeared after creation",
			fmt.Sprintf("The key %s was created but could not be read back.", id),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *globalAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state globalAPIKeyResourceModel
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

func (r *globalAPIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan globalAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings := r.settings(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.ID.ValueString()
	if err := r.client.UpdateGlobalAPIKey(ctx, id, settings.apply); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku global API key",
			fmt.Sprintf("Updating the key %s failed: %s", id, err),
		)
		return
	}

	current, err := r.client.GetGlobalAPIKey(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku global API key after updating it",
			fmt.Sprintf("Reading the key %s failed: %s", id, err),
		)
		return
	}
	if missing := settings.unknownAbilities(current); len(missing) > 0 {
		resp.Diagnostics.AddError("Unknown ability in permissions", unknownAbilitiesDetail(missing))
		return
	}

	resp.Diagnostics.Append(r.fill(ctx, current, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *globalAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state globalAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteGlobalAPIKey(ctx, id); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku global API key",
			fmt.Sprintf("Deleting the key %s failed: %s", id, err),
		)
	}
}

func (r *globalAPIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readInto refreshes model from the instance and reports whether the key
// still exists.
func (r *globalAPIKeyResource) readInto(ctx context.Context, id string, model *globalAPIKeyResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	key, err := r.client.GetGlobalAPIKey(ctx, id)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku global API key",
			fmt.Sprintf("Reading the key %s failed: %s", id, err),
		)
		return false, diags
	}
	diags.Append(r.fill(ctx, key, model)...)
	return !diags.HasError(), diags
}

// fill copies a key document into model. It never touches model.Key: the
// document carries the secret only in the create response, and a mask
// everywhere else, so the secret is set once in Create and otherwise left as
// it is.
func (r *globalAPIKeyResource) fill(ctx context.Context, key map[string]any, model *globalAPIKeyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Abilities are narrowed to the ones the configuration manages, so the
	// rest of what the instance reports does not show up as drift.
	global, _ := key["globalPermissions"].(map[string]any)
	permissions := map[string]bool{}
	for _, name := range managedPermissionKeys(ctx, model.Permissions, &diags) {
		permissions[name] = boolFromMap(global, name)
	}
	permissionsValue, d := types.MapValueFrom(ctx, types.BoolType, permissions)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	model.ID = types.StringValue(stringFromMap(key, "id"))
	model.Label = nullIfEmpty(stringFromMap(key, "label"))
	model.Description = nullIfEmpty(stringFromMap(key, "description"))
	model.UserForImpersonation = nullIfEmpty(stringFromMap(key, "dssUserForImpersonation"))
	model.Admin = types.BoolValue(boolFromMap(global, "admin"))
	model.Permissions = permissionsValue

	// A key is group-based when the field is there at all, empty or not.
	if _, groupBased := key["groups"]; groupBased {
		model.Groups = toStringSet(ctx, stringSliceFromMap(key, "groups"), &diags)
	} else {
		model.Groups = types.SetNull(types.StringType)
	}

	model.LifetimeDays = types.Int64Value(int64FromMap(key, "expiry"))
	model.ExpiresOn = types.StringNull()
	if ms := int64FromMap(key, "expiresOn"); ms > 0 {
		model.ExpiresOn = types.StringValue(time.UnixMilli(ms).UTC().Format(time.RFC3339))
	}
	return diags
}

// int64FromMap reads a whole number out of a decoded JSON document, where
// every number arrives as a float64.
func int64FromMap(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
