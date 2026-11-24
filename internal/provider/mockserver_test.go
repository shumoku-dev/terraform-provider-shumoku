package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// mockServer is an in-memory imitation of the Shumoku Server API, faithful to
// the semantics the provider depends on (partial PUTs, server-generated ids and
// secrets, credential deduplication, write-only credential secrets, the
// {code,message,requestId,error} error shape, Bearer auth and admin gating).
type mockServer struct {
	t     *testing.T
	mu    sync.Mutex
	srv   *httptest.Server
	token string
	// viewerToken authenticates but lacks admin:manage.
	viewerToken string
	seq         int
	webhookGen  int
	clock       int64

	topologies  map[string]map[string]any
	dataSources map[string]map[string]any
	dashboards  map[string]map[string]any
	sources     map[string]map[string]any
	credentials map[string]*mockCredential
}

type mockCredential struct {
	ID, Name, Method string
	Secret           map[string]string
	Topologies       []string
	Created, Changed int64
	Updated          int64
}

func newMockServer(t *testing.T) *mockServer {
	m := &mockServer{
		t:           t,
		token:       "shmk_test0000000000000000000000000000000000000",
		viewerToken: "shmk_viewer000000000000000000000000000000000",
		clock:       1700000000000,
		topologies:  map[string]map[string]any{},
		dataSources: map[string]map[string]any{},
		dashboards:  map[string]map[string]any{},
		sources:     map[string]map[string]any{},
		credentials: map[string]*mockCredential{},
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockServer) URL() string { return m.srv.URL }

func (m *mockServer) id(prefix string) string {
	m.seq++
	return fmt.Sprintf("%s%04d", prefix, m.seq)
}

func (m *mockServer) now() int64 {
	m.clock++
	return m.clock
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"code": code, "message": msg, "error": msg,
		"requestId": "00000000-0000-4000-8000-000000000001",
	})
}

func clone(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *mockServer) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p := strings.TrimPrefix(r.URL.Path, "/api")
	if p == r.URL.Path {
		apiErr(w, 404, "NOT_FOUND", "Route not found")
		return
	}
	if p == "/health" {
		writeJSON(w, 200, map[string]any{"status": "ok", "timestamp": m.clock})
		return
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if p == "/auth/status" {
		role, perms := "anonymous", []string{}
		switch auth {
		case m.token:
			role, perms = "admin", []string{"workspace:read", "workspace:write", "admin:manage"}
		case m.viewerToken:
			role, perms = "viewer", []string{"workspace:read"}
		}
		writeJSON(w, 200, map[string]any{"setupComplete": true, "authenticated": role != "anonymous",
			"subject": "api-token:x", "role": role, "authMethod": "bearer", "permissions": perms, "publicDemo": false})
		return
	}
	admin := strings.HasPrefix(p, "/datasources") || strings.HasPrefix(p, "/credentials") ||
		(strings.HasPrefix(p, "/topologies/") && strings.Contains(p, "/sources"))
	switch {
	case auth == m.token:
	case auth == m.viewerToken && !admin:
	case auth == m.viewerToken:
		apiErr(w, 403, "FORBIDDEN", "Permission required: admin:manage")
		return
	default:
		apiErr(w, 401, "UNAUTHORIZED", "Invalid or expired API token")
		return
	}

	var body map[string]any
	if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			apiErr(w, 400, "BAD_REQUEST", "invalid JSON body")
			return
		}
	}
	seg := strings.Split(strings.Trim(p, "/"), "/")

	switch {
	case seg[0] == "topologies" && len(seg) >= 3 && seg[2] == "sources":
		m.handleSources(w, r, seg, body)
	case seg[0] == "topologies":
		m.handleCRUD(w, r, seg, body, m.topologies, "topo", "Topology", func(obj map[string]any) {
			obj["compositionMode"] = "additive"
			obj["scopeMode"] = "auto"
			obj["scope"] = map[string]any{}
		})
	case seg[0] == "dashboards":
		m.handleCRUD(w, r, seg, body, m.dashboards, "dash", "Dashboard", func(obj map[string]any) {
			if _, ok := obj["layoutJson"]; !ok {
				obj["layoutJson"] = `{"columns":12,"rowHeight":100,"margin":8,"widgets":[]}`
			}
		})
	case seg[0] == "datasources" && len(seg) == 2 && seg[1] == "types":
		writeJSON(w, 200, []any{
			map[string]any{"type": "zabbix", "displayName": "Zabbix", "capabilities": []string{"topology", "metrics", "hosts", "alerts"},
				"configSchema": map[string]any{"type": "object", "required": []string{"url"}, "properties": map[string]any{
					"url":   map[string]any{"type": "string", "format": "uri"},
					"token": map[string]any{"type": "string", "secret": true},
				}}},
			map[string]any{"type": "grafana", "displayName": "Grafana", "capabilities": []string{"alerts"},
				"configSchema": map[string]any{"type": "object", "properties": map[string]any{
					"url":           map[string]any{"type": "string"},
					"useWebhook":    map[string]any{"type": "boolean"},
					"webhookSecret": map[string]any{"type": "string", "secret": true},
				}}},
		})
	case seg[0] == "datasources":
		m.handleDataSources(w, r, seg, body)
	case seg[0] == "credentials":
		m.handleCredentials(w, r, seg, body)
	default:
		apiErr(w, 404, "NOT_FOUND", "Route not found")
	}
}

// handleCRUD serves the list/get/create/partial-update/delete shape that
// topologies and dashboards share.
func (m *mockServer) handleCRUD(w http.ResponseWriter, r *http.Request, seg []string, body map[string]any,
	store map[string]map[string]any, prefix, kind string, defaults func(map[string]any)) {
	if len(seg) == 1 {
		switch r.Method {
		case http.MethodGet:
			list := []any{}
			for _, k := range sortedKeys(store) {
				list = append(list, store[k])
			}
			writeJSON(w, 200, list)
		case http.MethodPost:
			name, _ := body["name"].(string)
			if name == "" {
				apiErr(w, 400, "BAD_REQUEST", "name: Required")
				return
			}
			now := m.now()
			obj := clone(body)
			obj["id"] = m.id(prefix)
			obj["createdAt"] = now
			obj["updatedAt"] = now
			defaults(obj)
			store[obj["id"].(string)] = obj
			writeJSON(w, 201, obj)
		}
		return
	}
	obj, ok := store[seg[1]]
	if !ok {
		apiErr(w, 404, "NOT_FOUND", kind+" not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, obj)
	case http.MethodPut:
		for k, v := range body {
			obj[k] = v
		}
		obj["updatedAt"] = m.now()
		writeJSON(w, 200, obj)
	case http.MethodDelete:
		delete(store, seg[1])
		if kind == "Topology" {
			for id, s := range m.sources {
				if s["topologyId"] == seg[1] {
					delete(m.sources, id)
				}
			}
		}
		writeJSON(w, 200, map[string]any{"success": true})
	}
}

func (m *mockServer) handleDataSources(w http.ResponseWriter, r *http.Request, seg []string, body map[string]any) {
	// Like the real server: grafana with useWebhook gets a generated webhookSecret.
	normalize := func(typ, cfg string) (string, bool) {
		var c map[string]any
		if json.Unmarshal([]byte(cfg), &c) != nil {
			return "", false
		}
		if typ == "grafana" && c["useWebhook"] == true && c["webhookSecret"] == nil {
			m.webhookGen++
			c["webhookSecret"] = fmt.Sprintf("generated-webhook-secret-%d", m.webhookGen)
		}
		b, _ := json.Marshal(c)
		return string(b), true
	}
	setPolling := func(obj map[string]any, raw any) {
		pm, _ := raw.(map[string]any)
		if len(pm) == 0 {
			delete(obj, "polling")
			return
		}
		obj["polling"] = pm
	}
	if len(seg) == 1 {
		switch r.Method {
		case http.MethodGet:
			list := []any{}
			for _, k := range sortedKeys(m.dataSources) {
				list = append(list, m.dataSources[k])
			}
			writeJSON(w, 200, list)
		case http.MethodPost:
			typ, _ := body["type"].(string)
			cfg, _ := body["configJson"].(string)
			if typ != "zabbix" && typ != "grafana" {
				apiErr(w, 400, "BAD_REQUEST", "Unknown data source type: "+typ)
				return
			}
			norm, ok := normalize(typ, cfg)
			if !ok {
				apiErr(w, 400, "BAD_REQUEST", "configJson must be valid JSON")
				return
			}
			now := m.now()
			obj := map[string]any{"id": m.id("ds"), "name": body["name"], "type": typ, "configJson": norm,
				"status": "unknown", "failCount": 0, "createdAt": now, "updatedAt": now}
			setPolling(obj, body["polling"])
			m.dataSources[obj["id"].(string)] = obj
			writeJSON(w, 201, obj)
		}
		return
	}
	obj, ok := m.dataSources[seg[1]]
	if !ok {
		apiErr(w, 404, "NOT_FOUND", "Data source not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, obj)
	case http.MethodPut:
		if v, ok := body["name"]; ok {
			obj["name"] = v
		}
		if v, ok := body["type"]; ok {
			obj["type"] = v
		}
		if v, ok := body["configJson"].(string); ok {
			norm, ok := normalize(obj["type"].(string), v)
			if !ok {
				apiErr(w, 400, "BAD_REQUEST", "configJson must be valid JSON")
				return
			}
			obj["configJson"] = norm
		}
		if v, ok := body["polling"]; ok {
			setPolling(obj, v)
		}
		obj["updatedAt"] = m.now()
		writeJSON(w, 200, obj)
	case http.MethodDelete:
		delete(m.dataSources, seg[1])
		writeJSON(w, 200, map[string]any{"success": true})
	}
}

func (m *mockServer) handleSources(w http.ResponseWriter, r *http.Request, seg []string, body map[string]any) {
	topologyID := seg[1]
	if _, ok := m.topologies[topologyID]; !ok {
		apiErr(w, 404, "NOT_FOUND", "Topology not found")
		return
	}
	if len(seg) == 3 {
		switch r.Method {
		case http.MethodGet:
			list := []any{}
			for _, k := range sortedKeys(m.sources) {
				if m.sources[k]["topologyId"] == topologyID {
					list = append(list, m.sources[k])
				}
			}
			writeJSON(w, 200, list)
		case http.MethodPost:
			dsID, _ := body["dataSourceId"].(string)
			purpose, _ := body["purpose"].(string)
			if _, ok := m.dataSources[dsID]; !ok {
				apiErr(w, 404, "NOT_FOUND", "Data source not found")
				return
			}
			for _, s := range m.sources {
				if s["topologyId"] == topologyID && s["dataSourceId"] == dsID && s["purpose"] == purpose {
					apiErr(w, 409, "CONFLICT", "This data source is already linked with this purpose")
					return
				}
			}
			now := m.now()
			obj := map[string]any{"id": m.id("tds"), "topologyId": topologyID, "dataSourceId": dsID, "purpose": purpose,
				"syncMode": "manual", "priority": 0, "nodeContribution": "scoop", "linkContribution": "add",
				"createdAt": now, "updatedAt": now}
			m.applySourceUpdate(obj, body)
			m.sources[obj["id"].(string)] = obj
			// Like the real server, the POST response omits optionsJson and webhookSecret.
			echo := clone(obj)
			delete(echo, "optionsJson")
			delete(echo, "webhookSecret")
			writeJSON(w, 201, echo)
		}
		return
	}
	obj, ok := m.sources[seg[3]]
	if !ok || obj["topologyId"] != topologyID {
		apiErr(w, 404, "NOT_FOUND", "Topology data source not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		m.applySourceUpdate(obj, body)
		obj["updatedAt"] = m.now()
		writeJSON(w, 200, obj)
	case http.MethodDelete:
		delete(m.sources, seg[3])
		writeJSON(w, 200, map[string]any{"success": true})
	}
}

func (m *mockServer) applySourceUpdate(obj, body map[string]any) {
	for _, k := range []string{"syncMode", "priority", "nodeContribution", "linkContribution"} {
		if v, ok := body[k]; ok {
			obj[k] = v
		}
	}
	if v, ok := body["optionsJson"].(string); ok {
		if v == "" {
			delete(obj, "optionsJson")
		} else {
			obj["optionsJson"] = v
		}
	}
	if obj["syncMode"] == "webhook" {
		if _, ok := obj["webhookSecret"]; !ok {
			obj["webhookSecret"] = fmt.Sprintf("whsec-%d", m.now())
		}
	} else {
		delete(obj, "webhookSecret")
	}
}

func (m *mockServer) credentialView(c *mockCredential) map[string]any {
	topos := []any{}
	for _, id := range c.Topologies {
		name := ""
		if t, ok := m.topologies[id]; ok {
			name, _ = t["name"].(string)
		}
		topos = append(topos, map[string]any{"id": id, "name": name,
			"usage": map[string]any{"ok": 0, "partial": 0, "failed": 0, "pending": 0}, "silent": 0})
	}
	set := []string{}
	for k, v := range c.Secret {
		if v != "" {
			set = append(set, k)
		}
	}
	sort.Strings(set)
	return map[string]any{"id": c.ID, "name": c.Name, "method": c.Method, "params": map[string]any{},
		"secretsSet": set, "createdAt": c.Created, "updatedAt": c.Updated, "secretChangedAt": c.Changed, "topologies": topos}
}

func (m *mockServer) handleCredentials(w http.ResponseWriter, r *http.Request, seg []string, body map[string]any) {
	paramsOf := func() (map[string]string, bool) {
		out := map[string]string{}
		raw, _ := body["params"].(map[string]any)
		for k, v := range raw {
			s, ok := v.(string)
			if !ok {
				return nil, false
			}
			if k == "community" && strings.TrimSpace(s) != "" {
				out[k] = strings.TrimSpace(s)
			}
		}
		return out, true
	}
	topoIDs := func() ([]string, bool) {
		raw, ok := body["topologyIds"].([]any)
		if !ok {
			return nil, false
		}
		var out []string
		for _, v := range raw {
			out = append(out, v.(string))
		}
		sort.Strings(out)
		return out, true
	}
	if len(seg) == 1 {
		switch r.Method {
		case http.MethodGet:
			list := []any{}
			for _, k := range sortedKeysCred(m.credentials) {
				list = append(list, m.credentialView(m.credentials[k]))
			}
			writeJSON(w, 200, map[string]any{
				"methods": []any{map[string]any{"id": "snmp", "label": "SNMP", "fields": []any{
					map[string]any{"key": "community", "label": "Community", "secret": true, "required": true}}}},
				"credentials": list,
			})
		case http.MethodPost:
			if body["method"] != "snmp" {
				apiErr(w, 400, "BAD_REQUEST", fmt.Sprintf("unknown access method: %v", body["method"]))
				return
			}
			params, ok := paramsOf()
			if !ok || params["community"] == "" {
				apiErr(w, 400, "BAD_REQUEST", "Community is required")
				return
			}
			topos, _ := topoIDs()
			for _, c := range m.credentials {
				if c.Method == "snmp" && c.Secret["community"] == params["community"] {
					for _, t := range topos {
						if !containsStr(c.Topologies, t) {
							c.Topologies = append(c.Topologies, t)
						}
					}
					sort.Strings(c.Topologies)
					writeJSON(w, 200, map[string]any{"linkedExisting": true, "credential": m.credentialView(c)})
					return
				}
			}
			name, _ := body["name"].(string)
			if name == "" {
				name = fmt.Sprintf("SNMP %d", len(m.credentials)+1)
			}
			for _, c := range m.credentials {
				if c.Name == name {
					apiErr(w, 409, "CONFLICT", fmt.Sprintf("a credential named '%s' already exists", name))
					return
				}
			}
			now := m.now()
			c := &mockCredential{ID: m.id("cr_"), Name: name, Method: "snmp", Secret: params,
				Topologies: append([]string{}, topos...), Created: now, Updated: now, Changed: now}
			m.credentials[c.ID] = c
			writeJSON(w, 201, map[string]any{"linkedExisting": false, "credential": m.credentialView(c)})
		}
		return
	}
	c, ok := m.credentials[seg[1]]
	if !ok {
		apiErr(w, 404, "NOT_FOUND", "credential not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if name, ok := body["name"].(string); ok {
			c.Name = name
		}
		if params, ok := paramsOf(); ok && params["community"] != "" && params["community"] != c.Secret["community"] {
			c.Secret["community"] = params["community"]
			c.Changed = m.now()
		}
		if topos, ok := topoIDs(); ok {
			c.Topologies = append([]string{}, topos...)
		}
		c.Updated = m.now()
		writeJSON(w, 200, map[string]any{"credential": m.credentialView(c), "effect": []any{}})
	case http.MethodDelete:
		delete(m.credentials, c.ID)
		writeJSON(w, 200, map[string]any{"unassigned": 0, "topologies": len(c.Topologies)})
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysCred(m map[string]*mockCredential) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
