package client

import (
	"context"
	"net/http"
)

// Health calls GET /api/health (public).
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	_, err := c.do(ctx, http.MethodGet, "/health", nil, &out)
	return &out, err
}

// --- Topologies ---

// ListTopologies calls GET /api/topologies.
func (c *Client) ListTopologies(ctx context.Context) ([]Topology, error) {
	var out []Topology
	_, err := c.do(ctx, http.MethodGet, "/topologies", nil, &out)
	return out, err
}

// GetTopology calls GET /api/topologies/{id}.
func (c *Client) GetTopology(ctx context.Context, id string) (*Topology, error) {
	var out Topology
	if _, err := c.do(ctx, http.MethodGet, "/topologies/"+pathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateTopology calls POST /api/topologies.
func (c *Client) CreateTopology(ctx context.Context, req CreateTopologyRequest) (*Topology, error) {
	var out Topology
	if _, err := c.do(ctx, http.MethodPost, "/topologies", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTopology calls PUT /api/topologies/{id} (partial: omitted fields are kept).
func (c *Client) UpdateTopology(ctx context.Context, id string, req UpdateTopologyRequest) (*Topology, error) {
	var out Topology
	if _, err := c.do(ctx, http.MethodPut, "/topologies/"+pathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTopology calls DELETE /api/topologies/{id}.
func (c *Client) DeleteTopology(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/topologies/"+pathEscape(id), nil, nil)
	return err
}

// --- Data sources (admin:manage) ---

// ListDataSources calls GET /api/datasources.
func (c *Client) ListDataSources(ctx context.Context) ([]DataSource, error) {
	var out []DataSource
	_, err := c.do(ctx, http.MethodGet, "/datasources", nil, &out)
	return out, err
}

// ListDataSourceTypes calls GET /api/datasources/types.
func (c *Client) ListDataSourceTypes(ctx context.Context) ([]DataSourcePlugin, error) {
	var out []DataSourcePlugin
	_, err := c.do(ctx, http.MethodGet, "/datasources/types", nil, &out)
	return out, err
}

// GetDataSource calls GET /api/datasources/{id}.
func (c *Client) GetDataSource(ctx context.Context, id string) (*DataSource, error) {
	var out DataSource
	if _, err := c.do(ctx, http.MethodGet, "/datasources/"+pathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDataSource calls POST /api/datasources.
func (c *Client) CreateDataSource(ctx context.Context, req CreateDataSourceRequest) (*DataSource, error) {
	var out DataSource
	if _, err := c.do(ctx, http.MethodPost, "/datasources", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDataSource calls PUT /api/datasources/{id} (partial: omitted fields are kept;
// configJson, when sent, replaces the whole config).
func (c *Client) UpdateDataSource(ctx context.Context, id string, req UpdateDataSourceRequest) (*DataSource, error) {
	var out DataSource
	if _, err := c.do(ctx, http.MethodPut, "/datasources/"+pathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDataSource calls DELETE /api/datasources/{id}.
func (c *Client) DeleteDataSource(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/datasources/"+pathEscape(id), nil, nil)
	return err
}

// --- Credentials (admin:manage) ---

// ListCredentials calls GET /api/credentials (all credentials plus the access methods).
func (c *Client) ListCredentials(ctx context.Context) (*CredentialList, error) {
	var out CredentialList
	if _, err := c.do(ctx, http.MethodGet, "/credentials", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCredential finds one credential in GET /api/credentials. There is no
// per-id GET; a missing id yields an *APIError with StatusCode 404.
func (c *Client) GetCredential(ctx context.Context, id string) (*Credential, error) {
	list, err := c.ListCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list.Credentials {
		if list.Credentials[i].ID == id {
			return &list.Credentials[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/api/credentials",
		Code: "NOT_FOUND", Message: "credential " + id + " not found",
	}
}

// CreateCredential calls POST /api/credentials. When an identical credential
// already exists the server links the topologies to it and returns
// LinkedExisting=true instead of creating one.
func (c *Client) CreateCredential(ctx context.Context, req CreateCredentialRequest) (*CreateCredentialResult, error) {
	if req.TopologyIDs == nil {
		req.TopologyIDs = []string{}
	}
	if req.Params == nil {
		req.Params = map[string]string{}
	}
	var out CreateCredentialResult
	if _, err := c.do(ctx, http.MethodPost, "/credentials", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCredential calls PATCH /api/credentials/{id}.
func (c *Client) UpdateCredential(ctx context.Context, id string, req UpdateCredentialRequest) (*UpdateCredentialResult, error) {
	var out UpdateCredentialResult
	if _, err := c.do(ctx, http.MethodPatch, "/credentials/"+pathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCredential calls DELETE /api/credentials/{id}.
func (c *Client) DeleteCredential(ctx context.Context, id string) (*DeleteCredentialResult, error) {
	var out DeleteCredentialResult
	if _, err := c.do(ctx, http.MethodDelete, "/credentials/"+pathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Topology sources (admin:manage) ---

func topologySourcesPath(topologyID string) string {
	return "/topologies/" + pathEscape(topologyID) + "/sources"
}

// ListTopologySources calls GET /api/topologies/{topologyId}/sources.
func (c *Client) ListTopologySources(ctx context.Context, topologyID string) ([]TopologySource, error) {
	var out []TopologySource
	_, err := c.do(ctx, http.MethodGet, topologySourcesPath(topologyID), nil, &out)
	return out, err
}

// GetTopologySource finds one attachment (by attachment id) in the topology's
// source list. There is no per-id GET; a missing attachment yields a 404 *APIError.
func (c *Client) GetTopologySource(ctx context.Context, topologyID, attachmentID string) (*TopologySource, error) {
	list, err := c.ListTopologySources(ctx, topologyID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == attachmentID {
			return &list[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/api" + topologySourcesPath(topologyID),
		Code: "NOT_FOUND", Message: "topology source " + attachmentID + " not found",
	}
}

// AddTopologySource calls POST /api/topologies/{topologyId}/sources with an
// existing data source.
func (c *Client) AddTopologySource(ctx context.Context, topologyID string, req AddTopologySourceRequest) (*TopologySource, error) {
	var out TopologySource
	if _, err := c.do(ctx, http.MethodPost, topologySourcesPath(topologyID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTopologySource calls PUT /api/topologies/{topologyId}/sources/{attachmentId} (partial).
func (c *Client) UpdateTopologySource(ctx context.Context, topologyID, attachmentID string, req UpdateTopologySourceRequest) (*TopologySource, error) {
	var out TopologySource
	if _, err := c.do(ctx, http.MethodPut, topologySourcesPath(topologyID)+"/"+pathEscape(attachmentID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTopologySource calls DELETE /api/topologies/{topologyId}/sources/{attachmentId}.
func (c *Client) DeleteTopologySource(ctx context.Context, topologyID, attachmentID string) error {
	_, err := c.do(ctx, http.MethodDelete, topologySourcesPath(topologyID)+"/"+pathEscape(attachmentID), nil, nil)
	return err
}

// --- Dashboards ---

// ListDashboards calls GET /api/dashboards.
func (c *Client) ListDashboards(ctx context.Context) ([]Dashboard, error) {
	var out []Dashboard
	_, err := c.do(ctx, http.MethodGet, "/dashboards", nil, &out)
	return out, err
}

// GetDashboard calls GET /api/dashboards/{id}.
func (c *Client) GetDashboard(ctx context.Context, id string) (*Dashboard, error) {
	var out Dashboard
	if _, err := c.do(ctx, http.MethodGet, "/dashboards/"+pathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDashboard calls POST /api/dashboards.
func (c *Client) CreateDashboard(ctx context.Context, req CreateDashboardRequest) (*Dashboard, error) {
	var out Dashboard
	if _, err := c.do(ctx, http.MethodPost, "/dashboards", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDashboard calls PUT /api/dashboards/{id} (partial).
func (c *Client) UpdateDashboard(ctx context.Context, id string, req UpdateDashboardRequest) (*Dashboard, error) {
	var out Dashboard
	if _, err := c.do(ctx, http.MethodPut, "/dashboards/"+pathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDashboard calls DELETE /api/dashboards/{id}.
func (c *Client) DeleteDashboard(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/dashboards/"+pathEscape(id), nil, nil)
	return err
}
