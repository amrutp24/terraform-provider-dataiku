package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	_ resource.Resource                     = (*userResource)(nil)
	_ resource.ResourceWithConfigure        = (*userResource)(nil)
	_ resource.ResourceWithImportState      = (*userResource)(nil)
	_ resource.ResourceWithConfigValidators = (*userResource)(nil)
)

// NewUserResource returns the dataiku_user resource.
func NewUserResource() resource.Resource { return &userResource{} }

type userResource struct {
	client *dataiku.Client
}

type userResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Login             types.String `tfsdk:"login"`
	Password          types.String `tfsdk:"password"`
	PasswordWO        types.String `tfsdk:"password_wo"`
	PasswordWOVersion types.String `tfsdk:"password_wo_version"`
	DisplayName       types.String `tfsdk:"display_name"`
	Email             types.String `tfsdk:"email"`
	SourceType        types.String `tfsdk:"source_type"`
	UserProfile       types.String `tfsdk:"user_profile"`
	Groups            types.List   `tfsdk:"groups"`
	Enabled           types.Bool   `tfsdk:"enabled"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A user on a Dataiku DSS instance. Requires an API key with admin rights.\n\n" +
			"This resource is not available on Dataiku Cloud, where user management is handled by the " +
			"Cloud control plane rather than the instance's public API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The user's login. Same value as `login`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"login": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Login of the user. Changing this forces a new user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Password for a `LOCAL` user. DSS never returns the password, so this " +
					"provider cannot detect a password changed outside Terraform; it only writes the value " +
					"when it changes in configuration. Leave unset for `LDAP` users.\n\n" +
					"**This value is stored in Terraform state.** Prefer `password_wo`, which is not.",
			},
			"password_wo": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				MarkdownDescription: "The same as `password`, but never written to plan or state. Terraform " +
					"reads it from configuration at apply time and discards it.\n\n" +
					"Because nothing is retained, the provider cannot tell that the value changed. Bump " +
					"`password_wo_version` to make it send the password again — that is what a rotation " +
					"looks like:\n\n" +
					"```\npassword_wo         = ephemeral.some_secret.pw.value\npassword_wo_version = \"2\"\n```\n\n" +
					"Requires Terraform 1.11 or later. Conflicts with `password`.",
			},
			"password_wo_version": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "An arbitrary marker whose change tells the provider to send " +
					"`password_wo` again. Any value works — a counter, a date, a hash of the secret. " +
					"Required alongside `password_wo`, since without it a rotated password would never " +
					"reach the instance.",
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Human-readable name shown in the DSS interface.",
			},
			"email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Email address of the user.",
			},
			"source_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("LOCAL"),
				MarkdownDescription: "Where the user is defined. One of `LOCAL` or `LDAP`. Defaults to `LOCAL`. Changing this forces a new user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("LOCAL", "LDAP"),
				},
			},
			"user_profile": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Licence profile assigned to the user, for example `DESIGNER`, " +
					"`FULL_DESIGNER`, `DATA_DESIGNER`, `AI_CONSUMER` or `READER`. Which profiles exist depends " +
					"entirely on your Dataiku licence, so this provider neither restricts the value nor " +
					"defaults it: leave it unset and DSS assigns its own fallback profile, which is then read " +
					"back into state. Read `/public/api/admin/licensing/status` to see what your instance licenses.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"groups": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Names of the groups the user belongs to.\n\n" +
					"This list is the set of memberships Terraform manages, not necessarily the user's " +
					"complete membership. DSS attaches a per-user group of its own when a user is created " +
					"through the API, and an LDAP or SSO mapping can add more; those are left alone and are " +
					"not reported here, so they never show up as drift. Removing a group from this list does " +
					"remove that membership. On `terraform import` the user's full membership is adopted.",
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the user may sign in. Defaults to `true`.",
			},
		},
	}
}

func (r *userResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(
			path.MatchRoot("password"),
			path.MatchRoot("password_wo"),
		),
		// Without the version marker a rotated write-only password would be
		// read from configuration and then never sent, because nothing in
		// state changed to prompt an update.
		resourcevalidator.RequiredTogether(
			path.MatchRoot("password_wo"),
			path.MatchRoot("password_wo_version"),
		),
		resourcevalidator.PreferWriteOnlyAttribute(
			path.MatchRoot("password"),
			path.MatchRoot("password_wo"),
		),
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	login := plan.Login.ValueString()
	groups := fromStringList(ctx, plan.Groups, &resp.Diagnostics)
	// A write-only value exists only in configuration; it is null in the plan
	// by design, so it has to be read from there.
	password := writeOnlyString(ctx, req.Config, "password_wo", plan.Password, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := dataiku.CreateUserRequest{
		Login:       login,
		Password:    password,
		DisplayName: plan.DisplayName.ValueString(),
		SourceType:  plan.SourceType.ValueString(),
		Groups:      groups,
		UserProfile: plan.UserProfile.ValueString(),
		Email:       plan.Email.ValueString(),
	}

	if err := r.client.CreateUser(ctx, createReq); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku user",
			fmt.Sprintf("Creating user %q failed: %s", login, err),
		)
		return
	}

	// "enabled" is not accepted at creation time, so apply a non-default
	// value with a follow-up update.
	if !plan.Enabled.ValueBool() {
		err := r.client.UpdateUser(ctx, login, func(u map[string]any) {
			u["enabled"] = false
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to disable Dataiku user",
				fmt.Sprintf("The user %q was created but could not be disabled: %s", login, err),
			)
			return
		}
	}

	found, diags := r.readInto(ctx, login, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku user disappeared after creation",
			fmt.Sprintf("The user %q was created but could not be read back.", login),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readInto(ctx, state.Login.ValueString(), &state)
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

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	login := plan.Login.ValueString()
	groups := fromStringList(ctx, plan.Groups, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// The write-only password cannot be compared against prior state, since
	// none is kept; its version marker is what signals a rotation.
	passwordWO := writeOnlyString(ctx, req.Config, "password_wo", types.StringNull(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	rotateWriteOnly := passwordWO != "" && !plan.PasswordWOVersion.Equal(state.PasswordWOVersion)
	passwordChanged := !plan.Password.Equal(state.Password) && !plan.Password.IsNull()

	err := r.client.UpdateUser(ctx, login, func(u map[string]any) {
		u["displayName"] = plan.DisplayName.ValueString()
		u["email"] = plan.Email.ValueString()
		if profile := plan.UserProfile.ValueString(); profile != "" {
			u["userProfile"] = profile
		}
		u["groups"] = groups
		u["enabled"] = plan.Enabled.ValueBool()
		// DSS omits the password from GET, so only send it when it actually
		// changed; sending an empty string would clear it.
		switch {
		case rotateWriteOnly:
			u["password"] = passwordWO
		case passwordChanged:
			u["password"] = plan.Password.ValueString()
		}
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku user",
			fmt.Sprintf("Updating user %q failed: %s", login, err),
		)
		return
	}

	found, diags := r.readInto(ctx, login, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku user disappeared during update",
			fmt.Sprintf("The user %q could not be read back after being updated.", login),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	login := state.Login.ValueString()
	if err := r.client.DeleteUser(ctx, login); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku user",
			fmt.Sprintf("Deleting user %q failed: %s", login, err),
		)
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("login"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// readInto refreshes model from the instance, leaving password alone because
// DSS never returns it.
func (r *userResource) readInto(ctx context.Context, login string, model *userResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	user, err := r.client.GetUser(ctx, login)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku user",
			fmt.Sprintf("Reading user %q failed: %s", login, err),
		)
		return false, diags
	}

	model.ID = types.StringValue(login)
	model.Login = types.StringValue(login)
	model.DisplayName = types.StringValue(stringFromMap(user, "displayName"))
	model.Email = nullIfEmpty(stringFromMap(user, "email"))
	model.SourceType = types.StringValue(stringFromMap(user, "sourceType"))
	model.UserProfile = types.StringValue(stringFromMap(user, "userProfile"))
	model.Groups = managedGroupsList(ctx, model.Groups, stringSliceFromMap(user, "groups"), &diags)
	model.Enabled = types.BoolValue(boolFromMap(user, "enabled"))
	return true, diags
}

// managedGroupsList narrows the memberships DSS reports down to the ones the
// configuration actually names, so a group DSS attached on its own does not
// read back as drift. A null or unknown managed list means the resource is
// being imported, where adopting every membership is what the user wants.
func managedGroupsList(ctx context.Context, managed types.List, actual []string, diags *diag.Diagnostics) types.List {
	if managed.IsNull() || managed.IsUnknown() {
		return toStringList(ctx, actual, diags)
	}

	wanted := []string{}
	diags.Append(managed.ElementsAs(ctx, &wanted, false)...)
	if diags.HasError() {
		return toStringList(ctx, actual, diags)
	}

	present := make(map[string]bool, len(actual))
	for _, name := range actual {
		present[name] = true
	}

	// Keep the configured order so the list does not churn between reads.
	kept := make([]string, 0, len(wanted))
	for _, name := range wanted {
		if present[name] {
			kept = append(kept, name)
		}
	}
	return toStringList(ctx, kept, diags)
}
