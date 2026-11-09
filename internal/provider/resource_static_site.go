package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &staticSiteResource{}
	_ resource.ResourceWithConfigure   = &staticSiteResource{}
	_ resource.ResourceWithImportState = &staticSiteResource{}
)

func NewStaticSiteResource() resource.Resource { return &staticSiteResource{} }

type staticSiteResource struct{ client *client }

type staticSiteModel struct {
	ID            types.String `tfsdk:"id"`
	DomainName    types.String `tfsdk:"domain_name"`
	SiteUser      types.String `tfsdk:"site_user"`
	RootDirectory types.String `tfsdk:"root_directory"`
}

func (r *staticSiteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_static_site"
}

func (r *staticSiteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Static HTML site in CloudPanel (clpctl site:add:static). " +
			"Destroy runs clpctl site:delete, which removes the site user and the site files.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_name": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "Site domain, lowercase."},
			"site_user":   schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "Linux user of the site, unique on the host, up to 32 chars."},
			"root_directory": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "Directory name under /home/<site_user>/htdocs.",
			},
		},
	}
}

func (r *staticSiteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("%T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *staticSiteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan staticSiteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := plan.DomainName.ValueString()
	if err := r.client.addStaticSite(ctx, domain, plan.SiteUser.ValueString()); err != nil {
		resp.Diagnostics.AddError("Create site "+domain, err.Error())
		return
	}
	// clpctl may report validation errors with exit code 0, so trust the panel database.
	sites, err := r.client.listSites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Verify site "+domain, err.Error())
		return
	}
	s, ok := sites[domain]
	if !ok {
		resp.Diagnostics.AddError("Create site "+domain, "clpctl finished, but the site is not in the CloudPanel database")
		return
	}
	plan.ID = types.StringValue(domain)
	plan.RootDirectory = types.StringValue(s.RootDirectory)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *staticSiteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state staticSiteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sites, err := r.client.listSites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Read sites", err.Error())
		return
	}
	s, ok := sites[state.ID.ValueString()]
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}
	if s.Type != "static" {
		resp.Diagnostics.AddError("Site type mismatch", fmt.Sprintf("%s is a %q site in CloudPanel, not static", s.DomainName, s.Type))
		return
	}
	state.DomainName = types.StringValue(s.DomainName)
	state.SiteUser = types.StringValue(s.User)
	state.RootDirectory = types.StringValue(s.RootDirectory)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// All arguments force replacement, so Update is never called with changes.
func (r *staticSiteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan staticSiteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *staticSiteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state staticSiteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := state.ID.ValueString()
	if err := r.client.deleteSite(ctx, domain); err != nil {
		resp.Diagnostics.AddError("Delete site "+domain, err.Error())
		return
	}
	sites, err := r.client.listSites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Verify delete "+domain, err.Error())
		return
	}
	if _, ok := sites[domain]; ok {
		resp.Diagnostics.AddError("Delete site "+domain, "clpctl finished, but the site is still in the CloudPanel database")
	}
}

func (r *staticSiteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
