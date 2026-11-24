package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/shumoku-dev/terraform-provider-shumoku/internal/client"
)

var (
	_ resource.Resource                = &topologySourceResource{}
	_ resource.ResourceWithConfigure   = &topologySourceResource{}
	_ resource.ResourceWithImportState = &topologySourceResource{}
)

// NewTopologySourceResource returns the shumoku_topology_source resource.
func NewTopologySourceResource() resource.Resource { return &topologySourceResource{} }

type topologySourceResource struct{ client *client.Client }

type topologySourceModel struct {
	ID               types.String         `tfsdk:"id"`
	TopologyID       types.String         `tfsdk:"topology_id"`
	DataSourceID     types.String         `tfsdk:"data_source_id"`
	Purpose          types.String         `tfsdk:"purpose"`
	SyncMode         types.String         `tfsdk:"sync_mode"`
	Priority         types.Int64          `tfsdk:"priority"`
	OptionsJSON      jsontypes.Normalized `tfsdk:"options_json"`
	NodeContribution types.String         `tfsdk:"node_contribution"`
	LinkContribution types.String         `tfsdk:"link_contribution"`
	WebhookSecret    types.String         `tfsdk:"webhook_secret"`
	CreatedAt        types.Int64          `tfsdk:"created_at"`
}

func (m *topologySourceModel) fromAPI(s *client.TopologySource) {
	m.ID = types.StringValue(s.ID)
	m.TopologyID = types.StringValue(s.TopologyID)
	m.DataSourceID = types.StringValue(s.DataSourceID)
	m.Purpose = types.StringValue(s.Purpose)
	m.SyncMode = types.StringValue(s.SyncMode)
	m.Priority = types.Int64Value(s.Priority)
	if s.OptionsJSON == "" {
		m.OptionsJSON = jsontypes.NewNormalizedNull()
	} else {
		m.OptionsJSON = jsontypes.NewNormalizedValue(s.OptionsJSON)
	}
	m.NodeContribution = types.StringValue(s.NodeContribution)
	m.LinkContribution = types.StringValue(s.LinkContribution)
	m.WebhookSecret = types.StringValue(s.WebhookSecret)
	m.CreatedAt = types.Int64Value(s.CreatedAt)
}

func (r *topologySourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_topology_source"
}

func (r *topologySourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a `shumoku_data_source` to a `shumoku_topology`, as a topology source or a " +
			"metrics source. Requires an `admin` API token. A data source can be attached to a topology once per " +
			"purpose. Detaching a topology-purpose source also deletes the observations it contributed.\n\n" +
			"Import with `<topology_id>/<attachment_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-generated attachment id (not the data source id).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"topology_id": schema.StringAttribute{
				MarkdownDescription: "Topology id. Changing it forces a new attachment.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"data_source_id": schema.StringAttribute{
				MarkdownDescription: "Data source id. Changing it forces a new attachment.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"purpose": schema.StringAttribute{
				MarkdownDescription: "`topology` (the source contributes nodes and links) or `metrics` (it feeds " +
					"metrics). Changing it forces a new attachment.",
				Required:      true,
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf("topology", "metrics")},
			},
			"sync_mode": schema.StringAttribute{
				MarkdownDescription: "When the source is synced: `manual`, `on_view` or `webhook`. Defaults to `manual`. " +
					"`webhook` makes the server generate `webhook_secret`; leaving `webhook` clears it.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("manual"),
				Validators: []validator.String{stringvalidator.OneOf("manual", "on_view", "webhook")},
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Ordering among the topology's sources (lower first). Defaults to `0`.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(0),
			},
			"options_json": schema.StringAttribute{
				MarkdownDescription: "Plugin-specific sync options as a JSON string (use `jsonencode`); see the plugin's " +
					"`optionsSchema` in `shumoku_data_source_types`. Compared semantically.",
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
			},
			"node_contribution": schema.StringAttribute{
				MarkdownDescription: "How the source's nodes contribute: `scoop` (add them) or `anchor` (only enrich " +
					"nodes other sources have). Defaults to `scoop`.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("scoop"),
				Validators: []validator.String{stringvalidator.OneOf("scoop", "anchor")},
			},
			"link_contribution": schema.StringAttribute{
				MarkdownDescription: "How the source's links contribute: `add` or `update`. Defaults to `add`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("add"),
				Validators:          []validator.String{stringvalidator.OneOf("add", "update")},
			},
			"webhook_secret": schema.StringAttribute{
				MarkdownDescription: "Server-generated webhook secret when `sync_mode` is `webhook`, otherwise empty.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{keepWhileSyncModeUnchanged{}},
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Creation time, Unix milliseconds.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

// keepWhileSyncModeUnchanged keeps webhook_secret from state unless sync_mode
// changes, which is when the server generates or clears it.
type keepWhileSyncModeUnchanged struct{}

func (keepWhileSyncModeUnchanged) Description(context.Context) string {
	return "Keeps the prior value unless sync_mode changes."
}

func (m keepWhileSyncModeUnchanged) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepWhileSyncModeUnchanged) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() || req.Plan.Raw.IsNull() {
		return
	}
	var planMode, stateMode types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("sync_mode"), &planMode)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("sync_mode"), &stateMode)...)
	if !planMode.IsUnknown() && planMode.Equal(stateMode) {
		resp.PlanValue = req.StateValue
	}
}

func (r *topologySourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *topologySourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan topologySourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.AddTopologySourceRequest{
		DataSourceID:     plan.DataSourceID.ValueString(),
		Purpose:          plan.Purpose.ValueString(),
		SyncMode:         strPtr(plan.SyncMode.ValueString()),
		NodeContribution: strPtr(plan.NodeContribution.ValueString()),
		LinkContribution: strPtr(plan.LinkContribution.ValueString()),
	}
	priority := plan.Priority.ValueInt64()
	body.Priority = &priority
	if !plan.OptionsJSON.IsNull() && !plan.OptionsJSON.IsUnknown() {
		body.OptionsJSON = strPtr(plan.OptionsJSON.ValueString())
	}
	topologyID := plan.TopologyID.ValueString()
	src, err := r.client.AddTopologySource(ctx, topologyID, body)
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError("Data source already attached",
				fmt.Sprintf("Data source %s is already attached to topology %s with purpose %q. Import the existing "+
					"attachment with `terraform import <address> %s/<attachment_id>` (the attachment id is in "+
					"GET /api/topologies/%s/sources).\n\n%s",
					body.DataSourceID, topologyID, body.Purpose, topologyID, topologyID, err))
			return
		}
		addAPIError(&resp.Diagnostics, "attach data source to topology "+topologyID, err)
		return
	}
	// The server's POST response omits optionsJson and webhookSecret (it echoes
	// the inputs it built the row from), so read the stored attachment back.
	stored, err := r.client.GetTopologySource(ctx, topologyID, src.ID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read back topology source "+src.ID, err)
		// Keep the attachment in state so it is not orphaned.
		plan.fromAPI(src)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	plan.fromAPI(stored)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *topologySourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state topologySourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	src, err := r.client.GetTopologySource(ctx, state.TopologyID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		// Either the attachment or the whole topology is gone.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read topology source "+state.ID.ValueString(), err)
		return
	}
	state.fromAPI(src)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *topologySourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state topologySourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	priority := plan.Priority.ValueInt64()
	body := client.UpdateTopologySourceRequest{
		SyncMode:         strPtr(plan.SyncMode.ValueString()),
		Priority:         &priority,
		NodeContribution: strPtr(plan.NodeContribution.ValueString()),
		LinkContribution: strPtr(plan.LinkContribution.ValueString()),
		// An empty string clears stored options.
		OptionsJSON: strPtr(""),
	}
	if !plan.OptionsJSON.IsNull() && !plan.OptionsJSON.IsUnknown() {
		body.OptionsJSON = strPtr(plan.OptionsJSON.ValueString())
	}
	src, err := r.client.UpdateTopologySource(ctx, state.TopologyID.ValueString(), state.ID.ValueString(), body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update topology source "+state.ID.ValueString(), err)
		return
	}
	plan.fromAPI(src)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *topologySourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state topologySourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteTopologySource(ctx, state.TopologyID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "detach topology source "+state.ID.ValueString(), err)
	}
}

func (r *topologySourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	topologyID, attachmentID, ok := strings.Cut(req.ID, "/")
	if !ok || topologyID == "" || attachmentID == "" || strings.Contains(attachmentID, "/") {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("Expected <topology_id>/<attachment_id>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("topology_id"), topologyID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), attachmentID)...)
}
