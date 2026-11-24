package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/shumoku-dev/terraform-provider-shumoku/internal/client"
)

// ---------------------------------------------------------------- shumoku_topology

var (
	_ datasource.DataSource                     = &topologyDataSource{}
	_ datasource.DataSourceWithConfigure        = &topologyDataSource{}
	_ datasource.DataSourceWithConfigValidators = &topologyDataSource{}
)

// NewTopologyDataSource returns the shumoku_topology data source.
func NewTopologyDataSource() datasource.DataSource { return &topologyDataSource{} }

type topologyDataSource struct{ client *client.Client }

type topologyLookupModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	CompositionMode types.String `tfsdk:"composition_mode"`
	ScopeMode       types.String `tfsdk:"scope_mode"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
	UpdatedAt       types.Int64  `tfsdk:"updated_at"`
}

func (d *topologyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_topology"
}

func (d *topologyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a topology by `id` or by exact `name`. A name must match exactly one topology.",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{MarkdownDescription: "Topology id.", Optional: true, Computed: true},
			"name":             schema.StringAttribute{MarkdownDescription: "Exact topology name.", Optional: true, Computed: true},
			"composition_mode": schema.StringAttribute{MarkdownDescription: "`additive` or `enrichment`.", Computed: true},
			"scope_mode":       schema.StringAttribute{MarkdownDescription: "`auto`, `open` or `closed`.", Computed: true},
			"created_at":       schema.Int64Attribute{MarkdownDescription: "Creation time, Unix milliseconds.", Computed: true},
			"updated_at":       schema.Int64Attribute{MarkdownDescription: "Last update time, Unix milliseconds.", Computed: true},
		},
	}
}

func (d *topologyDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *topologyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *topologyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg topologyLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var found *client.Topology
	if !cfg.ID.IsNull() {
		t, err := d.client.GetTopology(ctx, cfg.ID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "read topology "+cfg.ID.ValueString(), err)
			return
		}
		found = t
	} else {
		list, err := d.client.ListTopologies(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "list topologies", err)
			return
		}
		name := cfg.Name.ValueString()
		var matches []client.Topology
		for _, t := range list {
			if t.Name == name {
				matches = append(matches, t)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Topology not found",
				fmt.Sprintf("No topology is named %q.", name))
			return
		case 1:
			found = &matches[0]
		default:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Topology name is ambiguous",
				fmt.Sprintf("%d topologies are named %q; look one up by id instead.", len(matches), name))
			return
		}
	}
	state := topologyLookupModel{
		ID:              types.StringValue(found.ID),
		Name:            types.StringValue(found.Name),
		CompositionMode: types.StringValue(found.CompositionMode),
		ScopeMode:       types.StringValue(found.ScopeMode),
		CreatedAt:       types.Int64Value(found.CreatedAt),
		UpdatedAt:       types.Int64Value(found.UpdatedAt),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------- shumoku_data_source

var (
	_ datasource.DataSource                     = &dataSourceDataSource{}
	_ datasource.DataSourceWithConfigure        = &dataSourceDataSource{}
	_ datasource.DataSourceWithConfigValidators = &dataSourceDataSource{}
)

// NewDataSourceDataSource returns the shumoku_data_source data source.
func NewDataSourceDataSource() datasource.DataSource { return &dataSourceDataSource{} }

type dataSourceDataSource struct{ client *client.Client }

type dataSourceLookupModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	Status types.String `tfsdk:"status"`
}

func (d *dataSourceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source"
}

func (d *dataSourceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a configured data source by `id` or by exact `name`, e.g. to attach one " +
			"that is managed outside Terraform. Its configuration (which holds secrets) is not exposed. " +
			"Requires an `admin` API token.",
		Attributes: map[string]schema.Attribute{
			"id":     schema.StringAttribute{MarkdownDescription: "Data source id.", Optional: true, Computed: true},
			"name":   schema.StringAttribute{MarkdownDescription: "Exact data source name.", Optional: true, Computed: true},
			"type":   schema.StringAttribute{MarkdownDescription: "Plugin type.", Computed: true},
			"status": schema.StringAttribute{MarkdownDescription: "`connected`, `disconnected` or `unknown`.", Computed: true},
		},
	}
}

func (d *dataSourceDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *dataSourceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *dataSourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dataSourceLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var found *client.DataSource
	if !cfg.ID.IsNull() {
		ds, err := d.client.GetDataSource(ctx, cfg.ID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "read data source "+cfg.ID.ValueString(), err)
			return
		}
		found = ds
	} else {
		list, err := d.client.ListDataSources(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "list data sources", err)
			return
		}
		name := cfg.Name.ValueString()
		var matches []client.DataSource
		for _, ds := range list {
			if ds.Name == name {
				matches = append(matches, ds)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Data source not found",
				fmt.Sprintf("No data source is named %q.", name))
			return
		case 1:
			found = &matches[0]
		default:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Data source name is ambiguous",
				fmt.Sprintf("%d data sources are named %q; look one up by id instead.", len(matches), name))
			return
		}
	}
	state := dataSourceLookupModel{
		ID:     types.StringValue(found.ID),
		Name:   types.StringValue(found.Name),
		Type:   types.StringValue(found.Type),
		Status: types.StringValue(found.Status),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------- shumoku_data_source_types

var (
	_ datasource.DataSource              = &dataSourceTypesDataSource{}
	_ datasource.DataSourceWithConfigure = &dataSourceTypesDataSource{}
)

// NewDataSourceTypesDataSource returns the shumoku_data_source_types data source.
func NewDataSourceTypesDataSource() datasource.DataSource { return &dataSourceTypesDataSource{} }

type dataSourceTypesDataSource struct{ client *client.Client }

type pluginTypeModel struct {
	Type              types.String `tfsdk:"type"`
	DisplayName       types.String `tfsdk:"display_name"`
	Capabilities      types.List   `tfsdk:"capabilities"`
	ConfigSchemaJSON  types.String `tfsdk:"config_schema_json"`
	OptionsSchemaJSON types.String `tfsdk:"options_schema_json"`
}

type dataSourceTypesModel struct {
	Types []pluginTypeModel `tfsdk:"types"`
}

func (d *dataSourceTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source_types"
}

func (d *dataSourceTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the data source plugin types the server has, with their capabilities and the " +
			"JSON schemas of their configuration (`shumoku_data_source.config_json`) and sync options " +
			"(`shumoku_topology_source.options_json`). Requires an `admin` API token.",
		Attributes: map[string]schema.Attribute{
			"types": schema.ListNestedAttribute{
				MarkdownDescription: "Registered plugin types.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":         schema.StringAttribute{MarkdownDescription: "Plugin type id, the value of `shumoku_data_source.type`.", Computed: true},
						"display_name": schema.StringAttribute{MarkdownDescription: "Human-readable name.", Computed: true},
						"capabilities": schema.ListAttribute{
							MarkdownDescription: "Capabilities, e.g. `topology`, `metrics`, `hosts`, `alerts`.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"config_schema_json": schema.StringAttribute{
							MarkdownDescription: "The plugin's `configSchema` as JSON (empty when it has none). Fields with " +
								"`secret: true` or `format: password` hold secrets.",
							Computed: true,
						},
						"options_schema_json": schema.StringAttribute{
							MarkdownDescription: "The plugin's `optionsSchema` (per-attachment sync options) as JSON, empty when it has none.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *dataSourceTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *dataSourceTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	plugins, err := d.client.ListDataSourceTypes(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list data source types", err)
		return
	}
	state := dataSourceTypesModel{Types: []pluginTypeModel{}}
	for _, p := range plugins {
		caps, diags := types.ListValueFrom(ctx, types.StringType, append([]string{}, p.Capabilities...))
		resp.Diagnostics.Append(diags...)
		state.Types = append(state.Types, pluginTypeModel{
			Type:              types.StringValue(p.Type),
			DisplayName:       types.StringValue(p.DisplayName),
			Capabilities:      caps,
			ConfigSchemaJSON:  types.StringValue(string(p.ConfigSchema)),
			OptionsSchemaJSON: types.StringValue(string(p.OptionsSchema)),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
