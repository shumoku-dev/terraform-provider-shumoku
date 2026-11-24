package provider

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &credentialResource{}
	_ resource.ResourceWithConfigure   = &credentialResource{}
	_ resource.ResourceWithImportState = &credentialResource{}
)

// NewCredentialResource returns the shumoku_credential resource.
func NewCredentialResource() resource.Resource { return &credentialResource{} }

type credentialResource struct{ client *client.Client }

type credentialModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Method          types.String `tfsdk:"method"`
	Params          types.Map    `tfsdk:"params"`
	SecretParams    types.Map    `tfsdk:"secret_params"`
	TopologyIDs     types.Set    `tfsdk:"topology_ids"`
	SecretsSet      types.Set    `tfsdk:"secrets_set"`
	CreatedAt       types.Int64  `tfsdk:"created_at"`
	SecretChangedAt types.Int64  `tfsdk:"secret_changed_at"`
}

func (r *credentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential"
}

func (r *credentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A server-wide device credential (for example an SNMP community) and the topologies " +
			"that use it. Requires an `admin` API token.\n\n" +
			"Secret parameters are write-only on the server: it reports which secret keys hold a value " +
			"(`secrets_set`) but never the values, so `secret_params` is kept from configuration and a change made " +
			"outside Terraform cannot be detected, except a secret that was cleared.\n\n" +
			"The server deduplicates credentials: creating one whose method and parameters (secrets included) " +
			"equal an existing credential's links the topologies to the existing one instead. This resource " +
			"refuses that case, undoes the links it caused, and asks you to import the existing credential.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-generated credential id (`cr_...`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique name. When omitted the server picks one (`SNMP 1`, `SNMP 2`, ...).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"method": schema.StringAttribute{
				MarkdownDescription: "Access method id, e.g. `snmp`. Changing it forces a new credential.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"params": schema.MapAttribute{
				MarkdownDescription: "Non-secret parameters of the method. The `snmp` method has none today. " +
					"The server keeps a stored value when a key is omitted, so removing a key does not clear it.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"secret_params": schema.MapAttribute{
				MarkdownDescription: "Secret parameters of the method, e.g. `{ community = \"...\" }` for `snmp`. " +
					"Write-only on the server; stored in Terraform state (mark your inputs sensitive). " +
					"A secret is never cleared by removing it here.",
				ElementType: types.StringType,
				Optional:    true,
				Sensitive:   true,
			},
			"topology_ids": schema.SetAttribute{
				MarkdownDescription: "Ids of the topologies that use this credential: the full set. Removing a " +
					"topology un-assigns the credential from that topology's devices. When omitted, the links are " +
					"not managed (whatever the server has is kept).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"secrets_set": schema.SetAttribute{
				MarkdownDescription: "Keys of the secret parameters that hold a value on the server.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Creation time, Unix milliseconds.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"secret_changed_at": schema.Int64Attribute{
				MarkdownDescription: "When the parameter values last changed, Unix milliseconds.",
				Computed:            true,
			},
		},
	}
}

func (r *credentialResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func mapFromModel(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out
	}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

func setFromModel(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

// validateParams checks the keys against the method's fields so a secret put
// in params (or vice versa) fails at apply with a clear message instead of an
// inconsistent-result error.
func validateParams(method client.AccessMethod, params, secrets map[string]string, diags *diag.Diagnostics) {
	fields := map[string]client.AccessField{}
	var allowedOpen, allowedSecret []string
	for _, f := range method.Fields {
		fields[f.Key] = f
		if f.Secret {
			allowedSecret = append(allowedSecret, f.Key)
		} else {
			allowedOpen = append(allowedOpen, f.Key)
		}
	}
	for k := range params {
		f, ok := fields[k]
		switch {
		case !ok:
			diags.AddAttributeError(path.Root("params"), "Unknown credential parameter",
				fmt.Sprintf("Method %q has no field %q. Non-secret fields: %v.", method.ID, k, allowedOpen))
		case f.Secret:
			diags.AddAttributeError(path.Root("params"), "Secret parameter in params",
				fmt.Sprintf("Field %q of method %q is secret; set it in secret_params instead.", k, method.ID))
		}
	}
	for k := range secrets {
		f, ok := fields[k]
		switch {
		case !ok:
			diags.AddAttributeError(path.Root("secret_params"), "Unknown credential parameter",
				fmt.Sprintf("Method %q has no field %q. Secret fields: %v.", method.ID, k, allowedSecret))
		case !f.Secret:
			diags.AddAttributeError(path.Root("secret_params"), "Non-secret parameter in secret_params",
				fmt.Sprintf("Field %q of method %q is not secret; set it in params instead.", k, method.ID))
		}
	}
}

// apply copies the server's view into the model. secret_params is not
// readable: it keeps the configured value, minus any key the server no longer
// holds, so a cleared secret shows up as a diff and is written again.
func applyCredential(ctx context.Context, m *credentialModel, c *client.Credential, diags *diag.Diagnostics) {
	m.ID = types.StringValue(c.ID)
	m.Name = types.StringValue(c.Name)
	m.Method = types.StringValue(c.Method)

	// params: the server trims values; keep the configured spelling when it matches.
	prior := mapFromModel(ctx, m.Params, diags)
	if len(c.Params) == 0 && (m.Params.IsNull() || len(prior) == 0) {
		if !m.Params.IsNull() && !m.Params.IsUnknown() {
			m.Params = types.MapValueMust(types.StringType, map[string]attr.Value{})
		} else {
			m.Params = types.MapNull(types.StringType)
		}
	} else {
		params := map[string]string{}
		for k, v := range c.Params {
			if pv, ok := prior[k]; ok && strings.TrimSpace(pv) == v {
				v = pv
			}
			params[k] = v
		}
		pm, d := types.MapValueFrom(ctx, types.StringType, params)
		diags.Append(d...)
		m.Params = pm
	}

	if !m.SecretParams.IsNull() && !m.SecretParams.IsUnknown() {
		secrets := mapFromModel(ctx, m.SecretParams, diags)
		for k := range secrets {
			if !slices.Contains(c.SecretsSet, k) {
				delete(secrets, k)
			}
		}
		sm, d := types.MapValueFrom(ctx, types.StringType, secrets)
		diags.Append(d...)
		m.SecretParams = sm
	}

	topos := c.TopologyIDs()
	sort.Strings(topos)
	ts, d := types.SetValueFrom(ctx, types.StringType, topos)
	diags.Append(d...)
	m.TopologyIDs = ts

	set := append([]string{}, c.SecretsSet...)
	sort.Strings(set)
	ss, d := types.SetValueFrom(ctx, types.StringType, set)
	diags.Append(d...)
	m.SecretsSet = ss

	m.CreatedAt = types.Int64Value(int64(c.CreatedAt))
	m.SecretChangedAt = types.Int64Value(int64(c.SecretChangedAt))
}

func (r *credentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan credentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	before, err := r.client.ListCredentials(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list credentials", err)
		return
	}
	methodID := plan.Method.ValueString()
	method, ok := before.Method(methodID)
	if !ok {
		var ids []string
		for _, m := range before.Methods {
			ids = append(ids, m.ID)
		}
		resp.Diagnostics.AddAttributeError(path.Root("method"), "Unknown access method",
			fmt.Sprintf("The server has no access method %q. Available: %v.", methodID, ids))
		return
	}
	params := mapFromModel(ctx, plan.Params, &resp.Diagnostics)
	secrets := mapFromModel(ctx, plan.SecretParams, &resp.Diagnostics)
	validateParams(method, params, secrets, &resp.Diagnostics)
	topologyIDs := setFromModel(ctx, plan.TopologyIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	all := map[string]string{}
	for k, v := range params {
		all[k] = v
	}
	for k, v := range secrets {
		all[k] = v
	}
	body := client.CreateCredentialRequest{Method: methodID, Params: all, TopologyIDs: topologyIDs}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		body.Name = plan.Name.ValueString()
	}
	res, err := r.client.CreateCredential(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create credential", err)
		return
	}

	if res.LinkedExisting {
		// The server linked our topologies to an identical, pre-existing credential.
		// Adopting it would let a later destroy delete a credential Terraform did not
		// create, so undo the links this call added and ask for an import instead.
		existing := res.Credential
		var previous []string
		for _, c := range before.Credentials {
			if c.ID == existing.ID {
				previous = c.TopologyIDs()
			}
		}
		now := existing.TopologyIDs()
		sort.Strings(previous)
		sort.Strings(now)
		rollback := ""
		if !slices.Equal(previous, now) {
			if previous == nil {
				previous = []string{}
			}
			if _, err := r.client.UpdateCredential(ctx, existing.ID, client.UpdateCredentialRequest{TopologyIDs: &previous}); err != nil {
				rollback = fmt.Sprintf("\n\nRolling back the topology links the server added failed (%s); check credential %s's topologies.", err, existing.ID)
			} else {
				rollback = "\n\nThe topology links the server added to it were removed again."
			}
		}
		resp.Diagnostics.AddError("Identical credential already exists",
			fmt.Sprintf("The server already has credential %q (id %s) with the same method and parameters; "+
				"it does not create duplicates. Import it instead:\n\n  terraform import <address> %s\n\n"+
				"or use different parameters.%s", existing.Name, existing.ID, existing.ID, rollback))
		return
	}

	applyCredential(ctx, &plan, &res.Credential, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state credentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetCredential(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read credential "+state.ID.ValueString(), err)
		return
	}
	applyCredential(ctx, &state, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *credentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state credentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := r.client.ListCredentials(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list credentials", err)
		return
	}
	params := mapFromModel(ctx, plan.Params, &resp.Diagnostics)
	secrets := mapFromModel(ctx, plan.SecretParams, &resp.Diagnostics)
	if method, ok := list.Method(plan.Method.ValueString()); ok {
		validateParams(method, params, secrets, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.UpdateCredentialRequest{}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		body.Name = strPtr(plan.Name.ValueString())
	}
	all := map[string]string{}
	for k, v := range params {
		all[k] = v
	}
	for k, v := range secrets {
		all[k] = v
	}
	if len(all) > 0 {
		body.Params = all
	}
	if !plan.TopologyIDs.IsNull() && !plan.TopologyIDs.IsUnknown() {
		ids := setFromModel(ctx, plan.TopologyIDs, &resp.Diagnostics)
		body.TopologyIDs = &ids
	}
	res, err := r.client.UpdateCredential(ctx, state.ID.ValueString(), body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update credential "+state.ID.ValueString(), err)
		return
	}
	applyCredential(ctx, &plan, &res.Credential, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state credentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.DeleteCredential(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete credential "+state.ID.ValueString(), err)
	}
}

func (r *credentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
