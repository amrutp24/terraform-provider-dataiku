package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

// ---------------------------------------------------------------------------
// dataiku_project
// ---------------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*projectDataSource)(nil)
)

// NewProjectDataSource returns the dataiku_project data source.
func NewProjectDataSource() datasource.DataSource { return &projectDataSource{} }

type projectDataSource struct {
	client *dataiku.Client
}

type projectDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectKey  types.String `tfsdk:"project_key"`
	Name        types.String `tfsdk:"name"`
	Owner       types.String `tfsdk:"owner"`
	Description types.String `tfsdk:"description"`
	ShortDesc   types.String `tfsdk:"short_desc"`
	Tags        types.List   `tfsdk:"tags"`
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Dataiku DSS project by key.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The project key."},
			"project_key": schema.StringAttribute{Required: true, MarkdownDescription: "Key of the project to read."},
			"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Display name of the project."},
			"owner":       schema.StringAttribute{Computed: true, MarkdownDescription: "Login of the project owner."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Long description of the project."},
			"short_desc":  schema.StringAttribute{Computed: true, MarkdownDescription: "Short description of the project."},
			"tags": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Tags applied to the project.",
			},
		},
	}
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(req, &resp.Diagnostics)
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectKey := config.ProjectKey.ValueString()

	metadata, err := d.client.GetProjectMetadata(ctx, projectKey)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku project",
			fmt.Sprintf("Reading metadata of project %q failed: %s", projectKey, err),
		)
		return
	}
	perms, err := d.client.GetProjectPermissions(ctx, projectKey)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku project permissions",
			fmt.Sprintf("Reading permissions of project %q failed: %s", projectKey, err),
		)
		return
	}

	config.ID = types.StringValue(projectKey)
	config.Name = types.StringValue(stringFromMap(metadata, "label"))
	config.Owner = types.StringValue(perms.Owner)
	config.Description = types.StringValue(stringFromMap(metadata, "description"))
	config.ShortDesc = types.StringValue(stringFromMap(metadata, "shortDesc"))
	config.Tags = toStringList(ctx, stringSliceFromMap(metadata, "tags"), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// ---------------------------------------------------------------------------
// dataiku_projects
// ---------------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*projectsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*projectsDataSource)(nil)
)

// NewProjectsDataSource returns the dataiku_projects data source.
func NewProjectsDataSource() datasource.DataSource { return &projectsDataSource{} }

type projectsDataSource struct {
	client *dataiku.Client
}

type projectsDataSourceModel struct {
	ID          types.String              `tfsdk:"id"`
	ProjectKeys types.List                `tfsdk:"project_keys"`
	Projects    []projectSummaryDataModel `tfsdk:"projects"`
}

type projectSummaryDataModel struct {
	ProjectKey  types.String `tfsdk:"project_key"`
	Name        types.String `tfsdk:"name"`
	Owner       types.String `tfsdk:"owner"`
	ShortDesc   types.String `tfsdk:"short_desc"`
	Description types.String `tfsdk:"description"`
	Tags        types.List   `tfsdk:"tags"`
}

func (d *projectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}

func (d *projectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Dataiku DSS project the API key is allowed to read.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The instance URL the projects were read from.",
			},
			"project_keys": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Keys of all readable projects, convenient for `for_each`.",
			},
			"projects": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per readable project.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"project_key": schema.StringAttribute{Computed: true, MarkdownDescription: "Key of the project."},
						"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Display name of the project."},
						"owner":       schema.StringAttribute{Computed: true, MarkdownDescription: "Login of the project owner."},
						"short_desc":  schema.StringAttribute{Computed: true, MarkdownDescription: "Short description of the project."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Long description of the project."},
						"tags": schema.ListAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Tags applied to the project.",
						},
					},
				},
			},
		},
	}
}

func (d *projectsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(req, &resp.Diagnostics)
}

func (d *projectsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	projects, err := d.client.ListProjects(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Dataiku projects", err.Error())
		return
	}

	state := projectsDataSourceModel{
		ID:       types.StringValue(d.client.Host()),
		Projects: make([]projectSummaryDataModel, 0, len(projects)),
	}
	keys := make([]string, 0, len(projects))

	for _, p := range projects {
		keys = append(keys, p.ProjectKey)
		state.Projects = append(state.Projects, projectSummaryDataModel{
			ProjectKey:  types.StringValue(p.ProjectKey),
			Name:        types.StringValue(p.DisplayName()),
			Owner:       types.StringValue(p.OwnerLogin),
			ShortDesc:   types.StringValue(p.ShortDesc),
			Description: types.StringValue(p.Description),
			Tags:        toStringList(ctx, p.Tags, &resp.Diagnostics),
		})
	}
	state.ProjectKeys = toStringList(ctx, keys, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------------------
// dataiku_user
// ---------------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*userDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*userDataSource)(nil)
)

// NewUserDataSource returns the dataiku_user data source.
func NewUserDataSource() datasource.DataSource { return &userDataSource{} }

type userDataSource struct {
	client *dataiku.Client
}

type userDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Login       types.String `tfsdk:"login"`
	DisplayName types.String `tfsdk:"display_name"`
	Email       types.String `tfsdk:"email"`
	SourceType  types.String `tfsdk:"source_type"`
	UserProfile types.String `tfsdk:"user_profile"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Groups      types.List   `tfsdk:"groups"`
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Dataiku DSS user by login. Requires an API key with admin rights.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "The user's login."},
			"login":        schema.StringAttribute{Required: true, MarkdownDescription: "Login of the user to read."},
			"display_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Human-readable name of the user."},
			"email":        schema.StringAttribute{Computed: true, MarkdownDescription: "Email address of the user."},
			"source_type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Where the user is defined, `LOCAL` or `LDAP`."},
			"user_profile": schema.StringAttribute{Computed: true, MarkdownDescription: "License profile assigned to the user."},
			"enabled":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user may sign in."},
			"groups": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Names of the groups the user belongs to.",
			},
		},
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(req, &resp.Diagnostics)
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	login := config.Login.ValueString()
	user, err := d.client.GetUser(ctx, login)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku user",
			fmt.Sprintf("Reading user %q failed: %s", login, err),
		)
		return
	}

	config.ID = types.StringValue(login)
	config.DisplayName = types.StringValue(stringFromMap(user, "displayName"))
	config.Email = types.StringValue(stringFromMap(user, "email"))
	config.SourceType = types.StringValue(stringFromMap(user, "sourceType"))
	config.UserProfile = types.StringValue(stringFromMap(user, "userProfile"))
	config.Enabled = types.BoolValue(boolFromMap(user, "enabled"))
	config.Groups = toStringList(ctx, stringSliceFromMap(user, "groups"), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// ---------------------------------------------------------------------------
// dataiku_group
// ---------------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*groupDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*groupDataSource)(nil)
)

// NewGroupDataSource returns the dataiku_group data source.
func NewGroupDataSource() datasource.DataSource { return &groupDataSource{} }

type groupDataSource struct {
	client *dataiku.Client
}

type groupDataSourceModel struct {
	ID             types.String         `tfsdk:"id"`
	Name           types.String         `tfsdk:"name"`
	Description    types.String         `tfsdk:"description"`
	SourceType     types.String         `tfsdk:"source_type"`
	Admin          types.Bool           `tfsdk:"admin"`
	LDAPGroupNames types.String         `tfsdk:"ldap_group_names"`
	DefinitionJSON jsontypes.Normalized `tfsdk:"definition_json"`
}

func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Dataiku DSS group by name. Requires an API key with admin rights.",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{Computed: true, MarkdownDescription: "The group name."},
			"name":             schema.StringAttribute{Required: true, MarkdownDescription: "Name of the group to read."},
			"description":      schema.StringAttribute{Computed: true, MarkdownDescription: "Description of the group."},
			"source_type":      schema.StringAttribute{Computed: true, MarkdownDescription: "Where the group is defined, `LOCAL` or `LDAP`."},
			"admin":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether members of the group are DSS administrators."},
			"ldap_group_names": schema.StringAttribute{Computed: true, MarkdownDescription: "LDAP group names mapped to this group."},
			"definition_json": schema.StringAttribute{
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "The group's full definition as returned by DSS, as a JSON object. " +
					"Use this to discover the ability field names your DSS version supports before " +
					"setting them in a `dataiku_group` resource's `permissions` map.",
			},
		},
	}
}

func (d *groupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(req, &resp.Diagnostics)
}

func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config groupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()
	group, err := d.client.GetGroup(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku group",
			fmt.Sprintf("Reading group %q failed: %s", name, err),
		)
		return
	}

	encoded, err := json.Marshal(group)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to encode Dataiku group definition",
			fmt.Sprintf("Encoding the definition of group %q failed: %s", name, err),
		)
		return
	}

	config.ID = types.StringValue(name)
	config.Description = types.StringValue(stringFromMap(group, "description"))
	config.SourceType = types.StringValue(stringFromMap(group, "sourceType"))
	config.Admin = types.BoolValue(boolFromMap(group, "admin"))
	config.LDAPGroupNames = types.StringValue(stringFromMap(group, "ldapGroupNames"))
	config.DefinitionJSON = jsontypes.NewNormalizedValue(string(encoded))
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// ---------------------------------------------------------------------------
// dataiku_connection
// ---------------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*connectionDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*connectionDataSource)(nil)
)

// NewConnectionDataSource returns the dataiku_connection data source.
func NewConnectionDataSource() datasource.DataSource { return &connectionDataSource{} }

type connectionDataSource struct {
	client *dataiku.Client
}

type connectionDataSourceModel struct {
	ID            types.String         `tfsdk:"id"`
	Name          types.String         `tfsdk:"name"`
	Type          types.String         `tfsdk:"type"`
	Description   types.String         `tfsdk:"description"`
	UsableBy      types.String         `tfsdk:"usable_by"`
	AllowedGroups types.List           `tfsdk:"allowed_groups"`
	ParamsJSON    jsontypes.Normalized `tfsdk:"params_json"`
}

func (d *connectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (d *connectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Dataiku DSS connection by name. Requires an API key with admin rights.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The connection name."},
			"name":        schema.StringAttribute{Required: true, MarkdownDescription: "Name of the connection to read."},
			"type":        schema.StringAttribute{Computed: true, MarkdownDescription: "Connection type, for example `PostgreSQL` or `S3`."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Description of the connection."},
			"usable_by":   schema.StringAttribute{Computed: true, MarkdownDescription: "Who may use the connection, `ALL` or `ALLOWED`."},
			"allowed_groups": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Groups allowed to use the connection.",
			},
			"params_json": schema.StringAttribute{
				Computed:   true,
				Sensitive:  true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Connection parameters as a JSON object. DSS redacts secret fields, " +
					"so passwords and keys are not readable through this attribute.",
			},
		},
	}
}

func (d *connectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceConfigure(req, &resp.Diagnostics)
}

func (d *connectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config connectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()
	conn, err := d.client.GetConnection(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Dataiku connection",
			fmt.Sprintf("Reading connection %q failed: %s", name, err),
		)
		return
	}

	params, ok := conn["params"]
	if !ok || params == nil {
		params = map[string]any{}
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to encode connection parameters",
			fmt.Sprintf("Encoding the parameters of connection %q failed: %s", name, err),
		)
		return
	}

	config.ID = types.StringValue(name)
	config.Type = types.StringValue(stringFromMap(conn, "type"))
	config.Description = types.StringValue(stringFromMap(conn, "description"))
	config.UsableBy = types.StringValue(stringFromMap(conn, "usableBy"))
	config.AllowedGroups = toStringList(ctx, stringSliceFromMap(conn, "allowedGroups"), &resp.Diagnostics)
	config.ParamsJSON = jsontypes.NewNormalizedValue(string(encoded))
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
