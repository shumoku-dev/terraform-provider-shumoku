package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/shumoku-dev/terraform-provider-shumoku/internal/client"
)

var (
	_ resource.Resource                = &topologyResource{}
	_ resource.ResourceWithConfigure   = &topologyResource{}
	_ resource.ResourceWithImportState = &topologyResource{}
)

// NewTopologyResource returns the shumoku_topology resource.
func NewTopologyResource() resource.Resource { return &topologyResource{} }

type topologyResource struct{ client *client.Client }

type topologyModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	CompositionMode types.String `tfsdk:"composition_mode"`
	ScopeMode       types.String `tfsdk:"scope_mode"`
	ShareToken      types.String `tfsdk:"share_token"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
	UpdatedAt       types.Int64  `tfsdk:"updated_at"`
}

func (m *topologyModel) fromAPI(t *client.Topology) {
	m.ID = types.StringValue(t.ID)
	m.Name = types.StringValue(t.Name)
	m.CompositionMode = types.StringValue(t.CompositionMode)
	m.ScopeMode = types.StringValue(t.ScopeMode)
	m.ShareToken = types.StringValue(t.ShareToken)
	m.CreatedAt = types.Int64Value(t.CreatedAt)
	m.UpdatedAt = types.Int64Value(t.UpdatedAt)
}

func (r *topologyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_topology"
}

func (r *topologyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Shumoku topology: the shell that data sources are attached to " +
			"(see `shumoku_topology_source`). Its graph content comes from the attached sources and the " +
			"Editor, which this resource does not manage.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-generated topology id.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"composition_mode": schema.StringAttribute{
				MarkdownDescription: "How source contributions compose (`additive` or `enrichment`). Read-only here; " +
					"set in the server UI or with `PUT /api/topologies/{id}/composition`.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"scope_mode": schema.StringAttribute{
				MarkdownDescription: "Scope mode (`auto`, `open` or `closed`). Read-only here.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"share_token": schema.StringAttribute{
				MarkdownDescription: "Public share token, empty when sharing is disabled. Read-only here.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       keep,
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Creation time, Unix milliseconds.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.Int64Attribute{
				MarkdownDescription: "Last update time, Unix milliseconds.",
				Computed:            true,
			},
		},
	}
}

func (r *topologyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *topologyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan topologyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.CreateTopology(ctx, client.CreateTopologyRequest{Name: plan.Name.ValueString()})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create topology", err)
		return
	}
	plan.fromAPI(t)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *topologyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state topologyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.GetTopology(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read topology "+state.ID.ValueString(), err)
		return
	}
	state.fromAPI(t)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *topologyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state topologyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.UpdateTopology(ctx, state.ID.ValueString(), client.UpdateTopologyRequest{Name: strPtr(plan.Name.ValueString())})
	if err != nil {
		addAPIError(&resp.Diagnostics, "update topology "+state.ID.ValueString(), err)
		return
	}
	plan.fromAPI(t)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *topologyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state topologyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteTopology(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete topology "+state.ID.ValueString(), err)
	}
}

func (r *topologyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
