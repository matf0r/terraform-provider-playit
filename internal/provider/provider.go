// Package provider implements the Terraform provider for playit.gg.
package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

const (
	secretKeyEnvVar      = "PLAYIT_SECRET_KEY"
	defaultCreateTimeout = 3 * time.Minute
)

var _ provider.Provider = (*playitProvider)(nil)

type playitProvider struct {
	version string
}

// New returns the provider factory used by main and by the test harness.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &playitProvider{version: version}
	}
}

// providerConfig is handed to every resource through ConfigureResponse.
type providerConfig struct {
	client        *playit.Client
	createTimeout time.Duration
}

type providerModel struct {
	SecretKey     types.String `tfsdk:"secret_key"`
	APIBase       types.String `tfsdk:"api_base"`
	CreateTimeout types.String `tfsdk:"tunnel_create_timeout"`
}

func (p *playitProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "playit"
	resp.Version = p.version
}

func (p *playitProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages [playit.gg](https://playit.gg) tunnels. " +
			"The provider drives the playit control plane only; it does not install or supervise the playit agent.",
		Attributes: map[string]schema.Attribute{
			"secret_key": schema.StringAttribute{
				MarkdownDescription: "playit agent secret key. May also be supplied through the `" +
					secretKeyEnvVar + "` environment variable. Find it with `playit secret-path`.",
				Optional:  true,
				Sensitive: true,
			},
			"api_base": schema.StringAttribute{
				MarkdownDescription: "Base URL of the playit API. Defaults to `" + playit.DefaultBaseURL +
					"`. Intended for testing.",
				Optional: true,
			},
			"tunnel_create_timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for a newly created tunnel to be allocated a " +
					"public address, as a Go duration. Defaults to `3m`.",
				Optional: true,
			},
		},
	}
}

func (p *playitProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Keep the key out of every log sink before it is ever handled.
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "secret_key")

	if config.SecretKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			pathSecretKey(),
			"playit secret key is not known at plan time",
			"The secret_key attribute depends on a value that is only known after apply. "+
				"Set it from a variable or from the "+secretKeyEnvVar+" environment variable instead.",
		)
		return
	}

	secretKey := os.Getenv(secretKeyEnvVar)
	if !config.SecretKey.IsNull() && config.SecretKey.ValueString() != "" {
		secretKey = config.SecretKey.ValueString()
	}
	if secretKey == "" {
		resp.Diagnostics.AddAttributeError(
			pathSecretKey(),
			"Missing playit secret key",
			"Set the secret_key attribute in the provider block, or export "+secretKeyEnvVar+".\n\n"+
				"The key is the one your playit agent already uses; locate it with `playit secret-path`.",
		)
		return
	}

	createTimeout := defaultCreateTimeout
	if !config.CreateTimeout.IsNull() && config.CreateTimeout.ValueString() != "" {
		parsed, err := time.ParseDuration(config.CreateTimeout.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				pathCreateTimeout(),
				"Invalid tunnel_create_timeout",
				"Expected a Go duration such as \"3m\" or \"90s\", got "+
					config.CreateTimeout.ValueString()+": "+err.Error(),
			)
			return
		}
		createTimeout = parsed
	}

	opts := []playit.Option{}
	if !config.APIBase.IsNull() && config.APIBase.ValueString() != "" {
		opts = append(opts, playit.WithBaseURL(config.APIBase.ValueString()))
	}
	client := playit.NewClient(secretKey, opts...)

	// Verify the credential up front so an invalid key fails here rather than
	// midway through an apply.
	if _, err := client.AgentsRunData(ctx); err != nil {
		if playit.IsAuth(err) {
			resp.Diagnostics.AddAttributeError(
				pathSecretKey(),
				"playit rejected the secret key",
				"The control plane refused the credential: "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Could not reach the playit API",
			"Verifying the credential against "+client.BaseURL()+" failed: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "playit provider configured", map[string]any{"api_base": client.BaseURL()})

	cfg := &providerConfig{client: client, createTimeout: createTimeout}
	resp.ResourceData = cfg
	resp.DataSourceData = cfg
}

func (p *playitProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewTunnelResource,
	}
}

func (p *playitProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
