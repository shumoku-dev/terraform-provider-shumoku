package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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
	_ resource.Resource                = &dashboardResource{}
	_ resource.ResourceWithConfigure   = &dashboardResource{}
	_ resource.ResourceWithImportState = &dashboardResource{}
)

// NewDashboardResource returns the shumoku_dashboard resource.
func NewDashboardResource() resource.Resource { return &dashboardResource{} }

type dashboardResource struct{ client *client.Client }

type dashboardModel struct {
	ID         types.String         `tfsdk:"id"`
	Name       types.String         `tfsdk:"name"`
	LayoutJSON jsontypes.Normalized `tfsdk:"layout_json"`
	ShareToken types.String         `tfsdk:"share_token"`
	CreatedAt  types.Int64          `tfsdk:"created_at"`
	UpdatedAt  types.Int64          `tfsdk:"updated_at"`
}

func (m *dashboardModel) fromAPI(d *client.Dashboard) {
	m.ID = types.StringValue(d.ID)
	m.Name = types.StringValue(d.Name)
	m.LayoutJSON = jsontypes.NewNormalizedValue(d.LayoutJSON)
	m.ShareToken = types.StringValue(d.ShareToken)
	m.CreatedAt = types.Int64Value(d.CreatedAt)
	m.UpdatedAt = types.Int64Value(d.UpdatedAt)
}

func (r *dashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *dashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Shumoku dashboard: a grid of widgets described by a layout JSON document.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-generated dashboard id.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"layout_json": schema.StringAttribute{
				MarkdownDescription: "The dashboard layout as a JSON string (use `jsonencode`). Shape as the server UI " +
					"writes it: `{columns, rowHeight, margin, widgets: [...]}`. Compared semantically, so key order and " +
					"whitespace do not cause diffs. When omitted, the server's default (an empty 12-column grid) is " +
					"used and then kept; layouts edited in the UI show up as drift when this is set.",
				CustomType:    jsontypes.NormalizedType{},
				Optional:      true,
				Computed:      true,
				PlanModifiers: keep,
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

func (r *dashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *dashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dashboardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.CreateDashboardRequest{Name: plan.Name.ValueString()}
	if !plan.LayoutJSON.IsNull() && !plan.LayoutJSON.IsUnknown() {
		body.LayoutJSON = strPtr(plan.LayoutJSON.ValueString())
	}
	d, err := r.client.CreateDashboard(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create dashboard", err)
		return
	}
	plan.fromAPI(d)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dashboardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.GetDashboard(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read dashboard "+state.ID.ValueString(), err)
		return
	}
	state.fromAPI(d)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dashboardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.UpdateDashboardRequest{Name: strPtr(plan.Name.ValueString())}
	if !plan.LayoutJSON.IsNull() && !plan.LayoutJSON.IsUnknown() {
		body.LayoutJSON = strPtr(plan.LayoutJSON.ValueString())
	}
	d, err := r.client.UpdateDashboard(ctx, state.ID.ValueString(), body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update dashboard "+state.ID.ValueString(), err)
		return
	}
	plan.fromAPI(d)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteDashboard(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete dashboard "+state.ID.ValueString(), err)
	}
}

func (r *dashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
