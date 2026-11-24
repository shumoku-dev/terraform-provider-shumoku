package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "shmk_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type recorded struct {
	method string
	path   string
	auth   string
	ctype  string
	body   string
}

// fakeServer answers each "METHOD /path" with a canned status and JSON body,
// recording every request.
func fakeServer(t *testing.T, routes map[string]struct {
	status int
	body   string
}) (*Client, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)})
		route, ok := routes[r.Method+" "+r.URL.Path]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"Route not found","requestId":"00000000-0000-4000-8000-000000000000","error":"Route not found"}`))
			return
		}
		w.WriteHeader(route.status)
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Endpoint: srv.URL + "/", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	return c, &reqs
}

type route = struct {
	status int
	body   string
}

func TestNewEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"https://shumoku.example.com":         "https://shumoku.example.com/api",
		"https://shumoku.example.com/":        "https://shumoku.example.com/api",
		"https://shumoku.example.com/api":     "https://shumoku.example.com/api",
		"https://example.com/shumoku/":        "https://example.com/shumoku/api",
		"http://127.0.0.1:8080/api/?x=1#frag": "http://127.0.0.1:8080/api",
	}
	for in, want := range cases {
		c, err := New(Config{Endpoint: in})
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := c.BaseURL(); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "shumoku.example.com", "https://"} {
		if _, err := New(Config{Endpoint: bad}); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

const topologyJSON = `{"id":"topo1","name":"Core","compositionMode":"additive","scopeMode":"auto","scope":{},"createdAt":1700000000000,"updatedAt":1700000000001}`

func TestTopologyCRUD(t *testing.T) {
	c, reqs := fakeServer(t, map[string]route{
		"POST /api/topologies":         {201, topologyJSON},
		"GET /api/topologies/topo1":    {200, topologyJSON},
		"PUT /api/topologies/topo1":    {200, strings.Replace(topologyJSON, `"Core"`, `"Edge"`, 1)},
		"DELETE /api/topologies/topo1": {200, `{"success":true}`},
		"GET /api/topologies":          {200, "[" + topologyJSON + "]"},
	})
	ctx := context.Background()
	created, err := c.CreateTopology(ctx, CreateTopologyRequest{Name: "Core"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "topo1" || created.CompositionMode != "additive" || created.CreatedAt != 1700000000000 {
		t.Fatalf("unexpected topology %+v", created)
	}
	name := "Edge"
	updated, err := c.UpdateTopology(ctx, "topo1", UpdateTopologyRequest{Name: &name})
	if err != nil || updated.Name != "Edge" {
		t.Fatalf("update: %v %+v", err, updated)
	}
	list, err := c.ListTopologies(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}
	if err := c.DeleteTopology(ctx, "topo1"); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if r.auth != "Bearer "+testToken {
		t.Errorf("auth header = %q", r.auth)
	}
	if r.ctype != "application/json" || r.body != `{"name":"Core"}` {
		t.Errorf("create request = %+v", r)
	}
	if (*reqs)[1].body != `{"name":"Edge"}` {
		t.Errorf("update body = %s", (*reqs)[1].body)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	c, _ := fakeServer(t, map[string]route{
		"GET /api/datasources": {403, `{"code":"FORBIDDEN","message":"Permission required: admin:manage","requestId":"11111111-1111-4111-8111-111111111111","error":"Permission required: admin:manage"}`},
		"GET /api/dashboards":  {502, `<html>bad gateway</html>`},
	})
	ctx := context.Background()
	_, err := c.ListDataSources(ctx)
	if !IsForbidden(err) {
		t.Fatalf("expected 403, got %v", err)
	}
	apiErr := err.(*APIError)
	if apiErr.Message != "Permission required: admin:manage" || apiErr.RequestID != "11111111-1111-4111-8111-111111111111" || apiErr.Code != "FORBIDDEN" {
		t.Errorf("parsed %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "requestId 11111111") {
		t.Errorf("error string %q lacks request id", err.Error())
	}
	_, err = c.ListDashboards(ctx)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 502 || !strings.Contains(apiErr.Error(), "bad gateway") {
		t.Errorf("non-JSON error: %v", err)
	}
	_, err = c.GetTopology(ctx, "missing")
	if !IsNotFound(err) {
		t.Errorf("expected not found, got %v", err)
	}
}

const dataSourceJSON = `{"id":"ds1","name":"Zabbix","type":"zabbix","configJson":"{\"url\":\"https://z\",\"token\":\"s3cret\"}","status":"unknown","failCount":0,"createdAt":1,"updatedAt":2,"polling":{"alertsMs":30000}}`

func TestDataSourceUpdateClearsPolling(t *testing.T) {
	c, reqs := fakeServer(t, map[string]route{
		"PUT /api/datasources/ds1":   {200, dataSourceJSON},
		"GET /api/datasources/types": {200, `[{"type":"zabbix","displayName":"Zabbix","capabilities":["topology","metrics"],"configSchema":{"type":"object","properties":{"url":{"type":"string"},"token":{"type":"string","secret":true}}}}]`},
	})
	ctx := context.Background()
	cfg := `{"url":"https://z"}`
	ds, err := c.UpdateDataSource(ctx, "ds1", UpdateDataSourceRequest{ConfigJSON: &cfg, Polling: &PollingOverride{}})
	if err != nil {
		t.Fatal(err)
	}
	if ds.Polling == nil || ds.Polling.AlertsMs == nil || *ds.Polling.AlertsMs != 30000 {
		t.Errorf("polling decode: %+v", ds.Polling)
	}
	var body map[string]json.RawMessage
	_ = json.Unmarshal([]byte((*reqs)[0].body), &body)
	if string(body["polling"]) != "{}" {
		t.Errorf("empty polling should be sent as {} to clear it, body %s", (*reqs)[0].body)
	}
	if _, ok := body["name"]; ok {
		t.Errorf("unset name must be omitted: %s", (*reqs)[0].body)
	}
	types, err := c.ListDataSourceTypes(ctx)
	if err != nil || len(types) != 1 || types[0].Type != "zabbix" || len(types[0].ConfigSchema) == 0 {
		t.Fatalf("types: %v %+v", err, types)
	}
}

const credentialJSON = `{"id":"cr_abc","name":"SNMP 1","method":"snmp","params":{},"secretsSet":["community"],"createdAt":1,"updatedAt":2,"secretChangedAt":1,"topologies":[{"id":"topo1","name":"Core","usage":{"ok":1,"partial":0,"failed":0,"pending":2},"silent":0}]}`

func TestCredentials(t *testing.T) {
	c, reqs := fakeServer(t, map[string]route{
		"GET /api/credentials":           {200, `{"methods":[{"id":"snmp","label":"SNMP","fields":[{"key":"community","label":"Community","secret":true,"required":true}]}],"credentials":[` + credentialJSON + `]}`},
		"POST /api/credentials":          {200, `{"linkedExisting":true,"credential":` + credentialJSON + `}`},
		"PATCH /api/credentials/cr_abc":  {200, `{"credential":` + credentialJSON + `,"effect":[]}`},
		"DELETE /api/credentials/cr_abc": {200, `{"unassigned":3,"topologies":1}`},
	})
	ctx := context.Background()
	cred, err := c.GetCredential(ctx, "cr_abc")
	if err != nil {
		t.Fatal(err)
	}
	if got := cred.TopologyIDs(); len(got) != 1 || got[0] != "topo1" || cred.SecretsSet[0] != "community" {
		t.Errorf("credential %+v", cred)
	}
	if _, err := c.GetCredential(ctx, "nope"); !IsNotFound(err) {
		t.Errorf("expected synthetic 404, got %v", err)
	}
	res, err := c.CreateCredential(ctx, CreateCredentialRequest{Method: "snmp", Params: map[string]string{"community": "public"}})
	if err != nil || !res.LinkedExisting {
		t.Fatalf("create: %v %+v", err, res)
	}
	if !strings.Contains((*reqs)[2].body, `"topologyIds":[]`) {
		t.Errorf("topologyIds must always be sent: %s", (*reqs)[2].body)
	}
	empty := []string{}
	if _, err := c.UpdateCredential(ctx, "cr_abc", UpdateCredentialRequest{TopologyIDs: &empty}); err != nil {
		t.Fatal(err)
	}
	if (*reqs)[3].body != `{"topologyIds":[]}` {
		t.Errorf("patch body %s", (*reqs)[3].body)
	}
	del, err := c.DeleteCredential(ctx, "cr_abc")
	if err != nil || del.Unassigned != 3 {
		t.Fatalf("delete: %v %+v", err, del)
	}
}

const sourceJSON = `{"id":"tds1","topologyId":"topo1","dataSourceId":"ds1","purpose":"topology","syncMode":"webhook","webhookSecret":"abcd","priority":0,"nodeContribution":"scoop","linkContribution":"add","createdAt":1,"updatedAt":1}`

func TestTopologySources(t *testing.T) {
	c, reqs := fakeServer(t, map[string]route{
		"GET /api/topologies/topo1/sources":         {200, "[" + sourceJSON + "]"},
		"POST /api/topologies/topo1/sources":        {201, sourceJSON},
		"PUT /api/topologies/topo1/sources/tds1":    {200, sourceJSON},
		"DELETE /api/topologies/topo1/sources/tds1": {200, `{"success":true}`},
	})
	ctx := context.Background()
	mode := "webhook"
	src, err := c.AddTopologySource(ctx, "topo1", AddTopologySourceRequest{DataSourceID: "ds1", Purpose: "topology", SyncMode: &mode})
	if err != nil || src.ID != "tds1" || src.WebhookSecret != "abcd" {
		t.Fatalf("add: %v %+v", err, src)
	}
	if (*reqs)[0].body != `{"dataSourceId":"ds1","purpose":"topology","syncMode":"webhook"}` {
		t.Errorf("add body %s", (*reqs)[0].body)
	}
	got, err := c.GetTopologySource(ctx, "topo1", "tds1")
	if err != nil || got.DataSourceID != "ds1" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := c.GetTopologySource(ctx, "topo1", "other"); !IsNotFound(err) {
		t.Errorf("expected 404, got %v", err)
	}
	empty := ""
	if _, err := c.UpdateTopologySource(ctx, "topo1", "tds1", UpdateTopologySourceRequest{OptionsJSON: &empty}); err != nil {
		t.Fatal(err)
	}
	if (*reqs)[3].body != `{"optionsJson":""}` {
		t.Errorf("update body %s", (*reqs)[3].body)
	}
	if err := c.DeleteTopologySource(ctx, "topo1", "tds1"); err != nil {
		t.Fatal(err)
	}
}

func TestDashboards(t *testing.T) {
	dash := `{"id":"d1","name":"NOC","layoutJson":"{\"columns\":12,\"rowHeight\":100,\"margin\":8,\"widgets\":[]}","createdAt":1,"updatedAt":1}`
	c, reqs := fakeServer(t, map[string]route{
		"POST /api/dashboards":      {201, dash},
		"GET /api/dashboards/d1":    {200, dash},
		"PUT /api/dashboards/d1":    {200, dash},
		"DELETE /api/dashboards/d1": {200, `{"success":true}`},
	})
	ctx := context.Background()
	d, err := c.CreateDashboard(ctx, CreateDashboardRequest{Name: "NOC"})
	if err != nil || d.ID != "d1" || !strings.Contains(d.LayoutJSON, "widgets") {
		t.Fatalf("create: %v %+v", err, d)
	}
	if (*reqs)[0].body != `{"name":"NOC"}` {
		t.Errorf("layout must be omitted when unset: %s", (*reqs)[0].body)
	}
	if _, err := c.GetDashboard(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDashboard(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
}
