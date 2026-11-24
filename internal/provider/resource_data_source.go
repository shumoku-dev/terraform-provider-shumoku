package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &dataSourceResource{}
	_ resource.ResourceWithConfigure   = &dataSourceResource{}
	_ resource.ResourceWithImportState = &dataSourceResource{}
)

// NewDataSourceResource returns the shumoku_data_source resource.
func NewDataSourceResource() resource.Resource { return &dataSourceResource{} }

type dataSourceResource struct{ client *client.Client }

type dataSourceModel struct {
	ID                  types.String         `tfsdk:"id"`
	Name                types.String         `tfsdk:"name"`
	Type                types.String         `tfsdk:"type"`
	ConfigJSON          jsontypes.Normalized `tfsdk:"config_json"`
	EffectiveConfigJSON types.String         `tfsdk:"effective_config_json"`
	PollingAlertsMs     types.Int64          `tfsdk:"polling_alerts_ms"`
	PollingMetricsMs    types.Int64          `tfsdk:"polling_metrics_ms"`
	CreatedAt           types.Int64          `tfsdk:"created_at"`
}

func (r *dataSourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source"
}

func (r *dataSourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Shumoku data source: a configured plugin instance (Zabbix, NetBox, Prometheus, " +
			"Grafana, ...) that topologies can attach. Requires an `admin` API token.\n\n" +
			"The plugin configuration is plugin-defined JSON; the `shumoku_data_source_types` data source lists " +
			"each plugin's `configSchema`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-generated data source id.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Plugin type, e.g. `zabbix`, `netbox`, `prometheus`, `grafana`. See " +
					"`shumoku_data_source_types`. Changing it updates the data source in place (its topology " +
					"attachments are kept), so change `config_json` together with it.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"config_json": schema.StringAttribute{
				MarkdownDescription: "Plugin configuration as a JSON object string (use `jsonencode`), validated by the " +
					"server against the plugin's `configSchema`. Sensitive, since it usually holds API tokens or " +
					"passwords. An update replaces the whole configuration.\n\n" +
					"Drift is judged only on the keys set here: keys the server adds itself (such as a generated Grafana " +
					"`webhookSecret`) are ignored, and secret fields the server returns empty or masked are treated as " +
					"unchanged. Compared semantically, so key order and whitespace do not cause diffs.",
				CustomType: jsontypes.NormalizedType{},
				Required:   true,
				Sensitive:  true,
			},
			"effective_config_json": schema.StringAttribute{
				MarkdownDescription: "The configuration as the server stores it, including keys it generated " +
					"(for example a Grafana `webhookSecret`). Use `jsondecode` to read a value.",
				Computed:  true,
				Sensitive: true,
			},
			"polling_alerts_ms": schema.Int64Attribute{
				MarkdownDescription: "Per-source alert polling interval override, in milliseconds. Unset uses the " +
					"server-wide interval.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"polling_metrics_ms": schema.Int64Attribute{
				MarkdownDescription: "Per-source metrics polling interval override, in milliseconds. Unset uses the " +
					"server-wide interval.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Creation time, Unix milliseconds.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dataSourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func pollingFromModel(m *dataSourceModel) *client.PollingOverride {
	p := &client.PollingOverride{}
	if !m.PollingAlertsMs.IsNull() && !m.PollingAlertsMs.IsUnknown() {
		v := m.PollingAlertsMs.ValueInt64()
		p.AlertsMs = &v
	}
	if !m.PollingMetricsMs.IsNull() && !m.PollingMetricsMs.IsUnknown() {
		v := m.PollingMetricsMs.ValueInt64()
		p.MetricsMs = &v
	}
	return p
}

// apply copies the server's view into the model. config_json is reconciled
// against the prior value so server-side additions and redactions don't diff.
func (r *dataSourceResource) apply(ctx context.Context, m *dataSourceModel, ds *client.DataSource) {
	prior := ""
	if !m.ConfigJSON.IsNull() && !m.ConfigJSON.IsUnknown() {
		prior = m.ConfigJSON.ValueString()
	}
	kept := reconcileConfigJSON(prior, ds.ConfigJSON, func() map[string]bool {
		return r.secretKeys(ctx, ds.Type)
	})
	m.ID = types.StringValue(ds.ID)
	m.Name = types.StringValue(ds.Name)
	m.Type = types.StringValue(ds.Type)
	m.ConfigJSON = jsontypes.NewNormalizedValue(kept)
	m.EffectiveConfigJSON = types.StringValue(ds.ConfigJSON)
	m.PollingAlertsMs = types.Int64Null()
	m.PollingMetricsMs = types.Int64Null()
	if ds.Polling != nil {
		m.PollingAlertsMs = types.Int64PointerValue(ds.Polling.AlertsMs)
		m.PollingMetricsMs = types.Int64PointerValue(ds.Polling.MetricsMs)
	}
	m.CreatedAt = types.Int64Value(ds.CreatedAt)
}

func (r *dataSourceResource) secretKeys(ctx context.Context, pluginType string) map[string]bool {
	plugins, err := r.client.ListDataSourceTypes(ctx)
	if err != nil {
		return nil
	}
	for _, p := range plugins {
		if p.Type == pluginType {
			return secretKeysFromConfigSchema(p.ConfigSchema)
		}
	}
	return nil
}

func validConfigObject(raw string) bool {
	var obj map[string]any
	return json.Unmarshal([]byte(raw), &obj) == nil
}

func (r *dataSourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dataSourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validConfigObject(plan.ConfigJSON.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("config_json"), "Invalid config_json",
			"config_json must be a JSON object, e.g. jsonencode({ url = \"...\" }).")
		return
	}
	ds, err := r.client.CreateDataSource(ctx, client.CreateDataSourceRequest{
		Name:       plan.Name.ValueString(),
		Type:       plan.Type.ValueString(),
		ConfigJSON: plan.ConfigJSON.ValueString(),
		Polling:    pollingFromModel(&plan),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create data source", err)
		return
	}
	r.apply(ctx, &plan, ds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataSourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataSourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ds, err := r.client.GetDataSource(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read data source "+state.ID.ValueString(), err)
		return
	}
	r.apply(ctx, &state, ds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dataSourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dataSourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validConfigObject(plan.ConfigJSON.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("config_json"), "Invalid config_json",
			"config_json must be a JSON object, e.g. jsonencode({ url = \"...\" }).")
		return
	}
	// PUT is partial on the server, but every managed field is sent: name, type,
	// the full config (it replaces the stored one) and the polling override (an
	// empty object clears it).
	ds, err := r.client.UpdateDataSource(ctx, state.ID.ValueString(), client.UpdateDataSourceRequest{
		Name:       strPtr(plan.Name.ValueString()),
		Type:       strPtr(plan.Type.ValueString()),
		ConfigJSON: strPtr(plan.ConfigJSON.ValueString()),
		Polling:    pollingFromModel(&plan),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "update data source "+state.ID.ValueString(), err)
		return
	}
	r.apply(ctx, &plan, ds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataSourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataSourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteDataSource(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete data source "+state.ID.ValueString(), err)
	}
}

func (r *dataSourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
