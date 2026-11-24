package client

import "encoding/json"

// Health is GET /api/health (public; used to check reachability).
type Health struct {
	Status string `json:"status"`
}

// Topology is the server's Topology schema.
type Topology struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	CompositionMode string `json:"compositionMode"`
	ScopeMode       string `json:"scopeMode"`
	ScopeSourceID   string `json:"scopeSourceId,omitempty"`
	MetricsSourceID string `json:"metricsSourceId,omitempty"`
	ShareToken      string `json:"shareToken,omitempty"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// CreateTopologyRequest is the CreateTopology schema.
type CreateTopologyRequest struct {
	Name string `json:"name"`
}

// UpdateTopologyRequest is the UpdateTopology schema (partial).
type UpdateTopologyRequest struct {
	Name *string `json:"name,omitempty"`
}

// PollingOverride is the PollingOverride schema. An empty object clears the override.
type PollingOverride struct {
	AlertsMs  *int64 `json:"alertsMs,omitempty"`
	MetricsMs *int64 `json:"metricsMs,omitempty"`
}

// DataSource is the server's DataSource schema.
type DataSource struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Type          string           `json:"type"`
	ConfigJSON    string           `json:"configJson"`
	Status        string           `json:"status"`
	StatusMessage string           `json:"statusMessage,omitempty"`
	LastCheckedAt *int64           `json:"lastCheckedAt,omitempty"`
	FailCount     int64            `json:"failCount"`
	CreatedAt     int64            `json:"createdAt"`
	UpdatedAt     int64            `json:"updatedAt"`
	Polling       *PollingOverride `json:"polling,omitempty"`
}

// CreateDataSourceRequest is the CreateDataSource schema.
type CreateDataSourceRequest struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	ConfigJSON string           `json:"configJson"`
	Polling    *PollingOverride `json:"polling,omitempty"`
}

// UpdateDataSourceRequest is the UpdateDataSource schema. Omitted fields are kept.
type UpdateDataSourceRequest struct {
	Name       *string          `json:"name,omitempty"`
	Type       *string          `json:"type,omitempty"`
	ConfigJSON *string          `json:"configJson,omitempty"`
	Polling    *PollingOverride `json:"polling,omitempty"`
}

// DataSourcePlugin is one entry of GET /api/datasources/types.
type DataSourcePlugin struct {
	Type          string          `json:"type"`
	DisplayName   string          `json:"displayName"`
	Capabilities  []string        `json:"capabilities"`
	ConfigSchema  json.RawMessage `json:"configSchema,omitempty"`
	OptionsSchema json.RawMessage `json:"optionsSchema,omitempty"`
}

// AccessField is a field of a credential access method.
type AccessField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

// AccessMethod is a way of reaching a device (e.g. snmp) and the fields its credential takes.
type AccessMethod struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Fields []AccessField `json:"fields"`
}

// CredentialUsage counts devices by read outcome.
type CredentialUsage struct {
	OK      int64 `json:"ok"`
	Partial int64 `json:"partial"`
	Failed  int64 `json:"failed"`
	Pending int64 `json:"pending"`
}

// CredentialTopology is a topology that uses a credential.
type CredentialTopology struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Usage  CredentialUsage `json:"usage"`
	Silent int64           `json:"silent"`
}

// Credential is the server's Credential schema. Secret values are never returned;
// SecretsSet lists the keys of secret fields that hold a value.
type Credential struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Method          string               `json:"method"`
	Params          map[string]string    `json:"params"`
	SecretsSet      []string             `json:"secretsSet"`
	CreatedAt       float64              `json:"createdAt"`
	UpdatedAt       float64              `json:"updatedAt"`
	SecretChangedAt float64              `json:"secretChangedAt"`
	Topologies      []CredentialTopology `json:"topologies"`
}

// TopologyIDs returns the ids of the topologies linked to the credential.
func (c *Credential) TopologyIDs() []string {
	ids := make([]string, 0, len(c.Topologies))
	for _, t := range c.Topologies {
		ids = append(ids, t.ID)
	}
	return ids
}

// CredentialList is GET /api/credentials.
type CredentialList struct {
	Methods     []AccessMethod `json:"methods"`
	Credentials []Credential   `json:"credentials"`
}

// Method returns the access method with the given id.
func (l *CredentialList) Method(id string) (AccessMethod, bool) {
	for _, m := range l.Methods {
		if m.ID == id {
			return m, true
		}
	}
	return AccessMethod{}, false
}

// CreateCredentialRequest is the CreateCredential schema. Params holds secret and
// non-secret fields together; the server splits them by the method's field flags.
type CreateCredentialRequest struct {
	Name        string            `json:"name,omitempty"`
	Method      string            `json:"method"`
	Params      map[string]string `json:"params"`
	TopologyIDs []string          `json:"topologyIds"`
}

// CreateCredentialResult is the CreateCredentialResult schema.
type CreateCredentialResult struct {
	// LinkedExisting is true when an identical credential (same method and
	// parameters, secrets included) already existed: the server linked the
	// requested topologies to it instead of creating a new one (HTTP 200).
	LinkedExisting bool       `json:"linkedExisting"`
	Credential     Credential `json:"credential"`
}

// UpdateCredentialRequest is the UpdateCredential schema. Omitted or empty
// param values keep what is stored. TopologyIDs, when set, is the full link set.
type UpdateCredentialRequest struct {
	Name        *string           `json:"name,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	TopologyIDs *[]string         `json:"topologyIds,omitempty"`
}

// CredentialEffect describes what a credential change touches in a topology.
type CredentialEffect struct {
	TopologyID  string `json:"topologyId"`
	Name        string `json:"name"`
	Devices     int64  `json:"devices"`
	RetrySilent int64  `json:"retrySilent"`
}

// UpdateCredentialResult is the UpdateCredentialResult schema.
type UpdateCredentialResult struct {
	Credential Credential         `json:"credential"`
	Effect     []CredentialEffect `json:"effect"`
}

// DeleteCredentialResult is the DeleteCredentialResult schema.
type DeleteCredentialResult struct {
	Unassigned int64 `json:"unassigned"`
	Topologies int64 `json:"topologies"`
}

// TopologySource is the TopologyDataSource schema: a data source attached to a topology.
type TopologySource struct {
	ID               string      `json:"id"`
	TopologyID       string      `json:"topologyId"`
	DataSourceID     string      `json:"dataSourceId"`
	Purpose          string      `json:"purpose"`
	SyncMode         string      `json:"syncMode"`
	WebhookSecret    string      `json:"webhookSecret,omitempty"`
	LastSyncedAt     *int64      `json:"lastSyncedAt,omitempty"`
	Priority         int64       `json:"priority"`
	OptionsJSON      string      `json:"optionsJson,omitempty"`
	NodeContribution string      `json:"nodeContribution"`
	LinkContribution string      `json:"linkContribution"`
	CreatedAt        int64       `json:"createdAt"`
	UpdatedAt        int64       `json:"updatedAt"`
	DataSource       *DataSource `json:"dataSource,omitempty"`
}

// AddTopologySourceRequest is the existing-data-source branch of AddTopologySource.
type AddTopologySourceRequest struct {
	DataSourceID     string  `json:"dataSourceId"`
	Purpose          string  `json:"purpose"`
	SyncMode         *string `json:"syncMode,omitempty"`
	Priority         *int64  `json:"priority,omitempty"`
	OptionsJSON      *string `json:"optionsJson,omitempty"`
	NodeContribution *string `json:"nodeContribution,omitempty"`
	LinkContribution *string `json:"linkContribution,omitempty"`
}

// UpdateTopologySourceRequest is the UpdateTopologySource schema (partial).
// An empty OptionsJSON clears the options.
type UpdateTopologySourceRequest struct {
	SyncMode         *string `json:"syncMode,omitempty"`
	Priority         *int64  `json:"priority,omitempty"`
	OptionsJSON      *string `json:"optionsJson,omitempty"`
	NodeContribution *string `json:"nodeContribution,omitempty"`
	LinkContribution *string `json:"linkContribution,omitempty"`
}

// Dashboard is the server's Dashboard schema.
type Dashboard struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	LayoutJSON string `json:"layoutJson"`
	ShareToken string `json:"shareToken,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// CreateDashboardRequest is the CreateDashboard schema. An omitted layout gets
// the server's default (an empty 12-column grid).
type CreateDashboardRequest struct {
	Name       string  `json:"name"`
	LayoutJSON *string `json:"layoutJson,omitempty"`
}

// UpdateDashboardRequest is the UpdateDashboard schema (partial).
type UpdateDashboardRequest struct {
	Name       *string `json:"name,omitempty"`
	LayoutJSON *string `json:"layoutJson,omitempty"`
}
