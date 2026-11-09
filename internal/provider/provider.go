package provider

import (
	"context"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &cloudpanelProvider{}

type cloudpanelProvider struct{ version string }

type cloudpanelProviderModel struct {
	InstanceID types.String `tfsdk:"instance_id"`
	Region     types.String `tfsdk:"region"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &cloudpanelProvider{version: version} }
}

func (p *cloudpanelProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudpanel"
	resp.Version = p.version
}

func (p *cloudpanelProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages CloudPanel sites on an EC2 instance through SSM Run Command documents " +
			"cloudpanel-site-list, cloudpanel-site-add-static and cloudpanel-site-delete. " +
			"AWS credentials come from the default AWS SDK chain.",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{Required: true, Description: "EC2 instance ID of the CloudPanel host."},
			"region":      schema.StringAttribute{Optional: true, Description: "AWS region of the instance. Env: AWS_REGION."},
		},
	}
}

func (p *cloudpanelProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg cloudpanelProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.InstanceID.IsUnknown() || cfg.InstanceID.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Missing instance_id", "instance_id must be a known, non-empty value.")
		return
	}

	region := cfg.Region.ValueString()
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		resp.Diagnostics.AddError("AWS configuration", err.Error())
		return
	}

	c := &client{
		ssm:          ssm.NewFromConfig(awsCfg),
		instanceID:   cfg.InstanceID.ValueString(),
		pollInterval: 2 * time.Second,
		timeout:      5 * time.Minute,
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *cloudpanelProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewStaticSiteResource}
}

func (p *cloudpanelProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
