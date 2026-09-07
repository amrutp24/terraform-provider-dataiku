package provider

import (
	"context"
	"fmt"
	"strings"

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
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*codeEnvResource)(nil)
	_ resource.ResourceWithConfigure   = (*codeEnvResource)(nil)
	_ resource.ResourceWithImportState = (*codeEnvResource)(nil)
)

// NewCodeEnvResource returns the dataiku_code_env resource.
func NewCodeEnvResource() resource.Resource { return &codeEnvResource{} }

type codeEnvResource struct {
	client *dataiku.Client
}

type codeEnvResourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Lang                    types.String `tfsdk:"lang"`
	DeploymentMode          types.String `tfsdk:"deployment_mode"`
	PythonInterpreter       types.String `tfsdk:"python_interpreter"`
	Conda                   types.Bool   `tfsdk:"conda"`
	Packages                types.String `tfsdk:"packages"`
	InstallCorePackages     types.Bool   `tfsdk:"install_core_packages"`
	CorePackagesSet         types.String `tfsdk:"core_packages_set"`
	InstallJupyterSupport   types.Bool   `tfsdk:"install_jupyter_support"`
	UsableByAll             types.Bool   `tfsdk:"usable_by_all"`
	InstallPackagesOnChange types.Bool   `tfsdk:"install_packages_on_change"`
}

func (r *codeEnvResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_code_env"
}

func (r *codeEnvResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A code environment on a Dataiku DSS instance. Requires an API key whose group " +
			"grants `mayCreateCodeEnvs`.\n\n" +
			"Creating the resource registers the environment; installing its packages is a separate, slow " +
			"step that runs pip or conda on the instance, so the instance needs outbound network access to " +
			"a package index. Set `install_packages_on_change` to `false` to manage the definition without " +
			"letting Terraform trigger builds.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<lang>/<name>`, for example `PYTHON/scikit`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the code environment. Changing this forces a new environment.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"lang": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Language of the environment, `PYTHON` or `R`. Changing this forces a new environment.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("PYTHON", "R"),
				},
			},
			"deployment_mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("DESIGN_MANAGED"),
				MarkdownDescription: "How the environment is deployed. `DESIGN_MANAGED` on a design node and " +
					"`AUTOMATION_SINGLE` or `AUTOMATION_VERSIONED` on an automation node. Changing this forces " +
					"a new environment.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"python_interpreter": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Interpreter to build a Python environment with, for example `PYTHON312` " +
					"or `PYTHON311`. Which interpreters exist depends on what is installed on the instance, so " +
					"this provider does not restrict the value. Ignored when `lang` is `R`. Changing this " +
					"forces a new environment.\n\n" +
					"**Set this rather than relying on the default.** DSS falls back to an interpreter of its " +
					"own choosing, and that version is frequently absent from a modern host: a DSS 15 " +
					"instance on Ubuntu 24.04, which ships Python 3.12 only, defaults to `python3.9` and the " +
					"build fails with `python3.9: command not found`. DSS reports the environment as created " +
					"regardless, so the failure surfaces later as a 500 about a missing `desc.json`.\n\n" +
					"Check what the instance has with `ls /usr/bin/python3*`. DSS 15 recognises `PYTHON34` " +
					"through `PYTHON315`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"conda": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Build the environment with conda rather than virtualenv. Changing this forces a new environment.",
				PlanModifiers: []planmodifier.Bool{
					boolRequiresReplace(),
				},
			},
			"packages": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Requested packages, in requirements.txt form: one specification per " +
					"line, for example `scikit-learn==1.5.0`. This is what you ask for, not what got " +
					"installed; DSS resolves it to a concrete set when the environment is built.",
			},
			"install_core_packages": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Install the core package set Dataiku ships alongside the requested packages.",
			},
			"core_packages_set": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Which core package set to install, for example `PANDAS23`. Only meaningful when `install_core_packages` is true.",
			},
			"install_jupyter_support": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Install the packages that let notebooks run against this environment.",
			},
			"usable_by_all": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether every user may select this environment. When false, DSS restricts it to the groups configured on the instance.",
			},
			"install_packages_on_change": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether Terraform asks DSS to resolve and install the packages after " +
					"creating the environment or changing `packages`. This runs pip or conda on the instance " +
					"and can take several minutes; set it to `false` to manage the definition only and build " +
					"the environment yourself. This argument is local to Terraform and is not stored on the instance.",
			},
		},
	}
}

func (r *codeEnvResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResourceConfigure(req, &resp.Diagnostics)
}

func (r *codeEnvResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan codeEnvResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	lang := plan.Lang.ValueString()
	name := plan.Name.ValueString()

	createReq := dataiku.CreateCodeEnvRequest{
		DeploymentMode: plan.DeploymentMode.ValueString(),
		Conda:          plan.Conda.ValueBool(),
	}
	if lang == "PYTHON" && !plan.PythonInterpreter.IsNull() && !plan.PythonInterpreter.IsUnknown() {
		createReq.PythonInterpreter = plan.PythonInterpreter.ValueString()
	}

	if err := r.client.CreateCodeEnv(ctx, lang, name, createReq); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Dataiku code environment",
			fmt.Sprintf("Creating %s code environment %q failed: %s", lang, name, err),
		)
		return
	}

	// The creation call does not carry the package list or the build options,
	// so apply them straight away.
	if err := r.applySettings(ctx, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Dataiku code environment was not usable after creation",
			r.creationFailureDetail(ctx, lang, name, err),
		)
		return
	}

	r.installPackages(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readInto(ctx, lang, name, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku code environment disappeared after creation",
			fmt.Sprintf("The code environment %q was created but could not be read back.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *codeEnvResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state codeEnvResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readInto(ctx, state.Lang.ValueString(), state.Name.ValueString(), &state)
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

func (r *codeEnvResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state codeEnvResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	lang := plan.Lang.ValueString()
	name := plan.Name.ValueString()

	if err := r.applySettings(ctx, &plan); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update Dataiku code environment",
			fmt.Sprintf("Updating code environment %q failed: %s", name, err),
		)
		return
	}

	// Only rebuild when the requested packages actually changed; a rebuild is
	// expensive and a rename of nothing should not trigger one.
	if !plan.Packages.Equal(state.Packages) {
		r.installPackages(ctx, &plan, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	found, diags := r.readInto(ctx, lang, name, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Dataiku code environment disappeared during update",
			fmt.Sprintf("The code environment %q could not be read back after being updated.", name),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *codeEnvResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state codeEnvResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	lang := state.Lang.ValueString()
	name := state.Name.ValueString()
	if err := r.client.DeleteCodeEnv(ctx, lang, name); err != nil && !dataiku.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Unable to delete Dataiku code environment",
			fmt.Sprintf("Deleting code environment %q failed: %s", name, err),
		)
	}
}

// ImportState accepts "<lang>/<name>", matching the resource's id.
func (r *codeEnvResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	lang, name, ok := strings.Cut(req.ID, "/")
	if !ok || lang == "" || name == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a code environment as \"<lang>/<name>\", for example \"PYTHON/scikit\", got %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("lang"), lang)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// applySettings writes the managed fields onto the environment's current
// settings document. DSS keeps most of them under "desc" while exposing some
// at the top level too, so each one is written wherever it already appears.
// creationFailureDetail explains a create that DSS accepted and then did not
// finish.
//
// Registering an environment and building its virtualenv are separate steps
// behind one call, and the call returns after the first. If the build then
// fails there is no environment, and every later request answers
//
//	HTTP 500 Internal Server Error: Not a file: /python/<name>/desc.json
//
// which describes a missing file three steps downstream of the cause and reads
// as though the environment exists but is misconfigured. DSS keeps the reason
// in the environment's build log, so fetch it and put it in front of the
// practitioner rather than making them find it on the instance.
func (r *codeEnvResource) creationFailureDetail(ctx context.Context, lang, name string, err error) string {
	var b strings.Builder

	fmt.Fprintf(&b, "DSS accepted the request to create %s code environment %q, then failed to "+
		"build it. Reading it back gave: %s", lang, name, err)

	if tail := r.client.CodeEnvFailureLog(ctx, lang, name, 12); tail != "" {
		fmt.Fprintf(&b, "\n\nThe end of the environment's build log on the instance:\n\n%s", tail)
	}

	if lang == "PYTHON" {
		b.WriteString("\n\nThe usual cause is an interpreter DSS cannot find. Left unset, " +
			"`python_interpreter` takes whatever the instance defaults to, which is often a " +
			"version the host does not have: a DSS 15 default of python3.9 on Ubuntu 24.04, " +
			"which ships 3.12 only, fails exactly this way. Set `python_interpreter` to a " +
			"version that is installed, such as PYTHON312.")
	}

	b.WriteString("\n\nNothing was written to state. The environment may still be registered " +
		"on the instance; delete it under Administration → Code envs before retrying if " +
		"the next apply reports that the name is taken.")

	return b.String()
}

func (r *codeEnvResource) applySettings(ctx context.Context, plan *codeEnvResourceModel) error {
	return r.client.UpdateCodeEnv(ctx, plan.Lang.ValueString(), plan.Name.ValueString(), func(env map[string]any) {
		if !plan.Packages.IsNull() && !plan.Packages.IsUnknown() {
			setCodeEnvField(env, "specPackageList", plan.Packages.ValueString())
		}
		if !plan.InstallCorePackages.IsNull() && !plan.InstallCorePackages.IsUnknown() {
			setCodeEnvField(env, "installCorePackages", plan.InstallCorePackages.ValueBool())
		}
		if !plan.CorePackagesSet.IsNull() && !plan.CorePackagesSet.IsUnknown() {
			setCodeEnvField(env, "corePackagesSet", plan.CorePackagesSet.ValueString())
		}
		if !plan.InstallJupyterSupport.IsNull() && !plan.InstallJupyterSupport.IsUnknown() {
			setCodeEnvField(env, "installJupyterSupport", plan.InstallJupyterSupport.ValueBool())
		}
		if !plan.UsableByAll.IsNull() && !plan.UsableByAll.IsUnknown() {
			setCodeEnvField(env, "usableByAll", plan.UsableByAll.ValueBool())
		}
	})
}

// installPackages asks DSS to resolve and install the environment's packages.
func (r *codeEnvResource) installPackages(ctx context.Context, plan *codeEnvResourceModel, diags *diag.Diagnostics) {
	if !plan.InstallPackagesOnChange.ValueBool() {
		return
	}

	lang := plan.Lang.ValueString()
	name := plan.Name.ValueString()
	tflog.Info(ctx, "installing code environment packages", map[string]any{"lang": lang, "name": name})

	result, err := r.client.UpdateCodeEnvPackages(ctx, lang, name)
	if err != nil {
		diags.AddError(
			"Unable to install code environment packages",
			fmt.Sprintf("DSS could not build code environment %q: %s\n\n"+
				"The instance needs outbound access to a package index for this step. Set "+
				"install_packages_on_change = false to manage the definition without building it.", name, err),
		)
		return
	}

	// DSS reports a failed build in the payload rather than with a non-2xx.
	if messages, ok := result["messages"].(map[string]any); ok {
		if failed, _ := messages["error"].(bool); failed {
			diags.AddError(
				"Dataiku could not build the code environment",
				fmt.Sprintf("Building code environment %q reported errors: %v", name, messages["messages"]),
			)
			return
		}
	}
}

func (r *codeEnvResource) readInto(ctx context.Context, lang, name string, model *codeEnvResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	env, err := r.client.GetCodeEnv(ctx, lang, name)
	if err != nil {
		if dataiku.IsNotFound(err) {
			return false, diags
		}
		diags.AddError(
			"Unable to read Dataiku code environment",
			fmt.Sprintf("Reading code environment %q failed: %s", name, err),
		)
		return false, diags
	}

	model.ID = types.StringValue(lang + "/" + name)
	model.Lang = types.StringValue(lang)
	model.Name = types.StringValue(name)
	model.DeploymentMode = types.StringValue(codeEnvString(env, "deploymentMode"))
	model.PythonInterpreter = types.StringValue(codeEnvString(env, "pythonInterpreter"))
	model.Conda = types.BoolValue(codeEnvBool(env, "conda"))
	model.Packages = types.StringValue(codeEnvString(env, "specPackageList"))
	model.InstallCorePackages = types.BoolValue(codeEnvBool(env, "installCorePackages"))
	model.CorePackagesSet = types.StringValue(codeEnvString(env, "corePackagesSet"))
	model.InstallJupyterSupport = types.BoolValue(codeEnvBool(env, "installJupyterSupport"))
	model.UsableByAll = types.BoolValue(codeEnvBool(env, "usableByAll"))

	// install_packages_on_change only exists in configuration, so a fresh
	// import has nothing to read it from.
	if model.InstallPackagesOnChange.IsNull() || model.InstallPackagesOnChange.IsUnknown() {
		model.InstallPackagesOnChange = types.BoolValue(true)
	}
	return true, diags
}

// setCodeEnvField writes a value everywhere the settings document already
// carries that key, since DSS mirrors some fields between the top level and
// the nested "desc" object and reads them back from "desc".
func setCodeEnvField(env map[string]any, key string, value any) {
	written := false
	if _, ok := env[key]; ok {
		env[key] = value
		written = true
	}
	if desc, ok := env["desc"].(map[string]any); ok {
		if _, ok := desc[key]; ok {
			desc[key] = value
			written = true
		}
	}
	if !written {
		env[key] = value
	}
}

// codeEnvString reads a field from "desc" first, falling back to the top level.
func codeEnvString(env map[string]any, key string) string {
	if desc, ok := env["desc"].(map[string]any); ok {
		if v, ok := desc[key].(string); ok {
			return v
		}
	}
	return stringFromMap(env, key)
}

func codeEnvBool(env map[string]any, key string) bool {
	if desc, ok := env["desc"].(map[string]any); ok {
		if v, ok := desc[key].(bool); ok {
			return v
		}
	}
	return boolFromMap(env, key)
}
