// Package provider implements the Shumoku Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/shumoku-dev/terraform-provider-shumoku/internal/client"
)

const (
	envEndpoint = "SHUMOKU_ENDPOINT"
	envToken    = "SHUMOKU_TOKEN"
	envInsecure = "SHUMOKU_INSECURE_SKIP_VERIFY"
)

var _ provider.Provider = &ShumokuProvider{}

// ShumokuProvider is the provider implementation.
type ShumokuProvider struct {
	version string
}

type providerModel struct {
	Endpoint           types.String `tfsdk:"endpoint"`
	Token              types.String `tfsdk:"token"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ShumokuProvider{version: version}
	}
}

func (p *ShumokuProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "shumoku"
	resp.Version = p.version
}

func (p *ShumokuProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a [Shumoku](https://shumoku.dev) Server — topologies, data sources, " +
			"credentials, topology source attachments and dashboards — through its REST API.\n\n" +
			"Authenticate with an API token (`shmk_...`) issued by a Shumoku administrator. Data sources, " +
			"credentials and topology source attachments are administrator surfaces: the token needs the " +
			"`admin` role (`admin:manage` permission) to manage them.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Base URL of the Shumoku Server, e.g. `https://shumoku.example.com`. " +
					"The provider appends `/api`. May also be set with the `" + envEndpoint + "` environment variable.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "API token (`shmk_...`), sent as `Authorization: Bearer <token>`. " +
					"May also be set with the `" + envToken + "` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"insecure_skip_verify": schema.BoolAttribute{
				MarkdownDescription: "Skip TLS certificate verification. Only for testing against servers with " +
					"self-signed certificates. May also be set with `" + envInsecure + "=true`. Defaults to `false`.",
				Optional: true,
			},
		},
	}
}

func (p *ShumokuProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Unknown Shumoku endpoint",
			"The provider cannot be configured because the endpoint is not known until apply. "+
				"Set it statically or with the "+envEndpoint+" environment variable.")
	}
	if cfg.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown Shumoku API token",
			"The provider cannot be configured because the token is not known until apply. "+
				"Set it statically or with the "+envToken+" environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := os.Getenv(envEndpoint)
	if !cfg.Endpoint.IsNull() {
		endpoint = cfg.Endpoint.ValueString()
	}
	token := os.Getenv(envToken)
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	insecure := os.Getenv(envInsecure) == "true" || os.Getenv(envInsecure) == "1"
	if !cfg.InsecureSkipVerify.IsNull() && !cfg.InsecureSkipVerify.IsUnknown() {
		insecure = cfg.InsecureSkipVerify.ValueBool()
	}

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Missing Shumoku endpoint",
			"Set the endpoint attribute or the "+envEndpoint+" environment variable to the server's base URL.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing Shumoku API token",
			"Set the token attribute or the "+envToken+" environment variable to an API token issued by a "+
				"Shumoku administrator.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := client.New(client.Config{
		Endpoint:           endpoint,
		Token:              token,
		InsecureSkipVerify: insecure,
		UserAgent:          "terraform-provider-shumoku/" + p.version,
	})
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid Shumoku endpoint", err.Error())
		return
	}

	// Check the token up front so a bad one fails once, clearly, instead of on every resource.
	status, err := c.AuthStatus(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "check the API token against "+c.BaseURL()+"/auth/status", err)
		return
	}
	if !status.SetupComplete {
		resp.Diagnostics.AddError("Shumoku Server setup incomplete",
			"The server at "+c.BaseURL()+" has no administrator yet. Finish its initial setup and issue an API token first.")
		return
	}
	if !status.Authenticated {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Shumoku authentication failed",
			"The server at "+c.BaseURL()+" did not accept the API token: it may be invalid, expired or revoked. "+
				"Check the token attribute or the "+envToken+" environment variable.")
		return
	}
	tflog.Debug(ctx, "Authenticated to Shumoku Server", map[string]any{
		"subject": status.Subject, "role": status.Role, "auth_method": status.AuthMethod,
	})
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *ShumokuProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewTopologyResource,
		NewDataSourceResource,
		NewCredentialResource,
		NewTopologySourceResource,
		NewDashboardResource,
	}
}

func (p *ShumokuProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewTopologyDataSource,
		NewDataSourceDataSource,
		NewDataSourceTypesDataSource,
	}
}
