package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

const (
	envHost     = "DATAIKU_HOST"
	envAPIKey   = "DATAIKU_API_KEY"
	envInsecure = "DATAIKU_INSECURE"
)

// Ensure the implementation satisfies the framework interfaces.
var _ provider.Provider = (*dataikuProvider)(nil)

type dataikuProvider struct {
	version string
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &dataikuProvider{version: version}
	}
}

type providerModel struct {
	Host           types.String `tfsdk:"host"`
	APIKey         types.String `tfsdk:"api_key"`
	Insecure       types.Bool   `tfsdk:"insecure"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
	MaxRetries     types.Int64  `tfsdk:"max_retries"`
}

func (p *dataikuProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dataiku"
	resp.Version = p.version
}

func (p *dataikuProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages objects on a Dataiku DSS instance through its public REST API.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL of the DSS instance, for example `https://dss.example.com`. " +
					"A trailing `/public/api` is accepted and ignored. May also be set with the `DATAIKU_HOST` environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "A DSS API key. Most resources in this provider require a key with admin rights. " +
					"May also be set with the `DATAIKU_API_KEY` environment variable, which is the recommended way to supply it.",
			},
			"insecure": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Skip TLS certificate verification. Only use this against instances with a self-signed " +
					"certificate. May also be set with the `DATAIKU_INSECURE` environment variable.",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Per-request timeout in seconds. Defaults to `60`.",
			},
			"max_retries": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "How many times to repeat a request that failed transiently — a " +
					"connection error, a `429`, or a `5xx`. Defaults to `3`; set `0` to disable retrying.\n\n" +
					"Only requests that are safe to repeat are retried. A `POST` is repeated solely on a " +
					"`429`, where DSS has said outright that it did not process the request, because a `5xx` " +
					"or a dropped connection on a create can mean the object was made and only the reply was " +
					"lost — repeating that would create a second one.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
		},
	}
}

func (p *dataikuProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Unknown values mean another resource has to be applied first; the
	// framework cannot build a client yet, so report a targeted error rather
	// than letting a nil client reach the resources.
	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			pathRoot("host"),
			"Unknown Dataiku host",
			"The provider cannot be configured because the host is not known until apply. "+
				"Set it to a static value or supply it through the "+envHost+" environment variable.",
		)
	}
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			pathRoot("api_key"),
			"Unknown Dataiku API key",
			"The provider cannot be configured because the API key is not known until apply. "+
				"Set it to a static value or supply it through the "+envAPIKey+" environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	host := firstNonEmpty(config.Host.ValueString(), os.Getenv(envHost))
	apiKey := firstNonEmpty(config.APIKey.ValueString(), os.Getenv(envAPIKey))

	insecure := config.Insecure.ValueBool()
	if config.Insecure.IsNull() {
		if v, err := strconv.ParseBool(os.Getenv(envInsecure)); err == nil {
			insecure = v
		}
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			pathRoot("host"),
			"Missing Dataiku host",
			"Set the provider's host argument or the "+envHost+" environment variable to the URL of your DSS instance.",
		)
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			pathRoot("api_key"),
			"Missing Dataiku API key",
			"Set the provider's api_key argument or the "+envAPIKey+" environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	timeout := 60 * time.Second
	if !config.TimeoutSeconds.IsNull() {
		secs := config.TimeoutSeconds.ValueInt64()
		if secs <= 0 {
			resp.Diagnostics.AddAttributeError(
				pathRoot("timeout_seconds"),
				"Invalid timeout",
				"timeout_seconds must be greater than zero.",
			)
			return
		}
		timeout = time.Duration(secs) * time.Second
	}

	// A negative budget is how the client is told to disable retrying, since
	// zero cannot be told apart from "unset" in a plain int.
	maxRetries := -1
	if !config.MaxRetries.IsNull() && !config.MaxRetries.IsUnknown() {
		if n := config.MaxRetries.ValueInt64(); n > 0 {
			maxRetries = int(n)
		}
	} else {
		maxRetries = 0 // let the client apply its default
	}

	// Naming the Terraform version as well as the provider gives whoever reads
	// the DSS access logs something actionable.
	userAgent := fmt.Sprintf("terraform-provider-dataiku/%s (+https://registry.terraform.io/providers/amrutp24/dataiku) Terraform/%s",
		p.version, req.TerraformVersion)

	client, err := dataiku.NewClient(dataiku.Config{
		Host:       host,
		APIKey:     apiKey,
		Insecure:   insecure,
		Timeout:    timeout,
		UserAgent:  userAgent,
		MaxRetries: maxRetries,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Dataiku API client", err.Error())
		return
	}

	// Fail configuration rather than every resource individually when the
	// host is unreachable or the key is rejected.
	if err := client.CheckConnectivity(ctx); err != nil {
		if dataiku.IsUnauthorized(err) {
			resp.Diagnostics.AddError(
				"Dataiku rejected the API key",
				"The instance at "+client.Host()+" refused the supplied credentials.\n\n"+err.Error(),
			)
		} else {
			resp.Diagnostics.AddError(
				"Unable to reach the Dataiku instance",
				"The provider could not complete an authenticated request against "+client.Host()+".\n\n"+err.Error(),
			)
		}
		return
	}

	tflog.Debug(ctx, "configured Dataiku client", map[string]any{"host": client.Host()})

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *dataikuProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewProjectPermissionsResource,
		NewProjectVariablesResource,
		NewProjectFolderResource,
		NewUserResource,
		NewGroupResource,
		NewConnectionResource,
		NewCodeEnvResource,
	}
}

func (p *dataikuProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProjectDataSource,
		NewProjectsDataSource,
		NewProjectFolderDataSource,
		NewUserDataSource,
		NewGroupDataSource,
		NewConnectionDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
