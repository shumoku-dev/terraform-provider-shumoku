package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccProtoV6ProviderFactories runs the provider in-process for both the
// mock-server tests and the TF_ACC acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"shumoku": providerserver.NewProtocol6WithError(New("test")()),
}

func mockProviderBlock(m *mockServer, token string) string {
	return fmt.Sprintf(`
provider "shumoku" {
  endpoint = %q
  token    = %q
}
`, m.URL(), token)
}

// requireTerraform skips the mock-server tests when no terraform binary can be found;
// terraform-plugin-testing drives the real CLI even for UnitTest.
func requireTerraform(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC_TERRAFORM_PATH") != "" {
		return
	}
	for _, dir := range filepathList(os.Getenv("PATH")) {
		if fi, err := os.Stat(dir + "/terraform"); err == nil && !fi.IsDir() {
			return
		}
	}
	t.Skip("terraform CLI not found on PATH; set TF_ACC_TERRAFORM_PATH to run provider tests")
}

func filepathList(path string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == os.PathListSeparator {
			if i > start {
				out = append(out, path[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func TestMockLifecycle(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	p := mockProviderBlock(m, m.token)

	step1 := p + `
resource "shumoku_topology" "core" {
  name = "Core"
}

resource "shumoku_data_source" "zabbix" {
  name        = "Zabbix"
  type        = "zabbix"
  config_json = jsonencode({ url = "https://zabbix.example.com", token = "s3cret" })
}

resource "shumoku_topology_source" "zabbix" {
  topology_id    = shumoku_topology.core.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "topology"
  options_json   = jsonencode({})
}

resource "shumoku_topology_source" "metrics_webhook" {
  topology_id    = shumoku_topology.core.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "metrics"
  sync_mode      = "webhook"
}

resource "shumoku_dashboard" "noc" {
  name = "NOC"
}

resource "shumoku_credential" "snmp" {
  name          = "Campus SNMP"
  method        = "snmp"
  secret_params = { community = "public" }
  topology_ids  = [shumoku_topology.core.id]
}

data "shumoku_topology" "by_name" {
  name       = "Core"
  depends_on = [shumoku_topology.core]
}

data "shumoku_data_source_types" "all" {}
`
	step2 := p + `
resource "shumoku_topology" "core" {
  name = "Core network"
}

resource "shumoku_data_source" "zabbix" {
  name               = "Zabbix prod"
  type               = "zabbix"
  config_json        = jsonencode({ token = "rotated", url = "https://zabbix.example.com" })
  polling_alerts_ms  = 30000
}

resource "shumoku_topology_source" "zabbix" {
  topology_id    = shumoku_topology.core.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "topology"
  sync_mode      = "webhook"
  priority       = 5
  options_json   = jsonencode({ includeLinks = true })
}

resource "shumoku_dashboard" "noc" {
  name        = "NOC wall"
  layout_json = jsonencode({ columns = 12, rowHeight = 80, margin = 8, widgets = [] })
}

resource "shumoku_credential" "snmp" {
  name          = "Campus SNMP v2"
  method        = "snmp"
  secret_params = { community = "private" }
  topology_ids  = []
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: step1,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("shumoku_topology.core", "id"),
					resource.TestCheckResourceAttr("shumoku_topology.core", "composition_mode", "additive"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "sync_mode", "manual"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "node_contribution", "scoop"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "webhook_secret", ""),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "options_json", "{}"),
					resource.TestCheckResourceAttrWith("shumoku_topology_source.metrics_webhook", "webhook_secret", nonEmpty),
					resource.TestCheckResourceAttrPair("data.shumoku_topology.by_name", "id", "shumoku_topology.core", "id"),
					resource.TestCheckResourceAttr("data.shumoku_data_source_types.all", "types.#", "2"),
					resource.TestCheckResourceAttr("shumoku_dashboard.noc", "layout_json",
						`{"columns":12,"rowHeight":100,"margin":8,"widgets":[]}`),
					resource.TestCheckResourceAttr("shumoku_credential.snmp", "secrets_set.#", "1"),
					resource.TestCheckResourceAttr("shumoku_credential.snmp", "secrets_set.0", "community"),
					resource.TestCheckResourceAttr("shumoku_credential.snmp", "topology_ids.#", "1"),
				),
			},
			{
				Config: step2,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("shumoku_topology.core", "name", "Core network"),
					resource.TestCheckResourceAttr("shumoku_data_source.zabbix", "polling_alerts_ms", "30000"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "priority", "5"),
					resource.TestCheckResourceAttrWith("shumoku_topology_source.zabbix", "webhook_secret", nonEmpty),
					resource.TestCheckResourceAttr("shumoku_credential.snmp", "name", "Campus SNMP v2"),
					resource.TestCheckResourceAttr("shumoku_credential.snmp", "topology_ids.#", "0"),
					func(*terraform.State) error {
						for _, c := range m.credentials {
							if c.Secret["community"] != "private" {
								return fmt.Errorf("secret not rotated: %q", c.Secret["community"])
							}
						}
						return nil
					},
				),
			},
			{ResourceName: "shumoku_topology.core", ImportState: true, ImportStateVerify: true},
			{ResourceName: "shumoku_dashboard.noc", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "shumoku_data_source.zabbix", ImportState: true, ImportStateVerify: true,
			},
			{
				ResourceName:      "shumoku_topology_source.zabbix",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["shumoku_topology_source.zabbix"]
					return rs.Primary.Attributes["topology_id"] + "/" + rs.Primary.ID, nil
				},
			},
			{
				ResourceName:            "shumoku_credential.snmp",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret_params"},
			},
			// Removing the polling override clears it; dropping sync_mode returns to the default.
			{
				Config: p + `
resource "shumoku_topology" "core" {
  name = "Core network"
}

resource "shumoku_data_source" "zabbix" {
  name        = "Zabbix prod"
  type        = "zabbix"
  config_json = jsonencode({ token = "rotated", url = "https://zabbix.example.com" })
}

resource "shumoku_topology_source" "zabbix" {
  topology_id    = shumoku_topology.core.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "topology"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("shumoku_data_source.zabbix", "polling_alerts_ms"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "sync_mode", "manual"),
					resource.TestCheckResourceAttr("shumoku_topology_source.zabbix", "webhook_secret", ""),
					resource.TestCheckNoResourceAttr("shumoku_topology_source.zabbix", "options_json"),
					func(*terraform.State) error {
						for _, ds := range m.dataSources {
							if _, ok := ds["polling"]; ok {
								return fmt.Errorf("polling override not cleared on the server: %v", ds["polling"])
							}
						}
						if len(m.credentials) != 0 || len(m.dashboards) != 0 {
							return fmt.Errorf("removed resources still on the server")
						}
						return nil
					},
				),
			},
		},
	})
}

func nonEmpty(v string) error {
	if v == "" {
		return fmt.Errorf("expected a value")
	}
	return nil
}

// A data source whose config the server augments (Grafana webhookSecret) must
// not show a perpetual diff, and drift on a configured key must be detected.
func TestMockDataSourceServerAddedKeys(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	cfg := mockProviderBlock(m, m.token) + `
resource "shumoku_data_source" "grafana" {
  name        = "Grafana"
  type        = "grafana"
  config_json = jsonencode({ url = "https://grafana.example.com", useWebhook = true })
}

output "webhook_secret" {
  value     = jsondecode(shumoku_data_source.grafana.effective_config_json).webhookSecret
  sensitive = true
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckOutput("webhook_secret", "generated-webhook-secret-1"),
				),
			},
			{
				// Updating the config must not make the server regenerate the webhook secret.
				Config: strings.Replace(cfg, "https://grafana.example.com", "https://grafana2.example.com", 1),
				Check:  resource.TestCheckOutput("webhook_secret", "generated-webhook-secret-1"),
			},
			{
				// Someone edits the URL in the UI: Terraform must see it and put it back.
				PreConfig: func() {
					m.mu.Lock()
					defer m.mu.Unlock()
					for _, ds := range m.dataSources {
						ds["configJson"] = `{"url":"https://changed.example.com","useWebhook":true,"webhookSecret":"generated-webhook-secret-1"}`
					}
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: cfg},
		},
	})
}

func TestMockOutOfBandDeletion(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	cfg := mockProviderBlock(m, m.token) + `
resource "shumoku_topology" "core" {
  name = "Core"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					m.mu.Lock()
					defer m.mu.Unlock()
					for id := range m.topologies {
						delete(m.topologies, id)
					}
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// The server links topologies to an identical existing credential instead of
// creating a duplicate; the resource must refuse and undo the links.
func TestMockCredentialLinkedExisting(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	base := mockProviderBlock(m, m.token) + `
resource "shumoku_topology" "a" {
  name = "A"
}

resource "shumoku_topology" "b" {
  name = "B"
}

resource "shumoku_credential" "first" {
  method        = "snmp"
  secret_params = { community = "public" }
  topology_ids  = [shumoku_topology.a.id]
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base,
				Check:  resource.TestCheckResourceAttr("shumoku_credential.first", "name", "SNMP 1"),
			},
			{
				Config: base + `
resource "shumoku_credential" "dupe" {
  name          = "Another name"
  method        = "snmp"
  secret_params = { community = "public" }
  topology_ids  = [shumoku_topology.b.id]
}
`,
				ExpectError: regexp.MustCompile(`Identical credential already exists`),
			},
			{
				Config: base,
				Check: func(*terraform.State) error {
					if len(m.credentials) != 1 {
						return fmt.Errorf("expected 1 credential, got %d", len(m.credentials))
					}
					for _, c := range m.credentials {
						if len(c.Topologies) != 1 {
							return fmt.Errorf("links were not rolled back: %v", c.Topologies)
						}
					}
					return nil
				},
			},
		},
	})
}

func TestMockCredentialValidation(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mockProviderBlock(m, m.token) + `
resource "shumoku_credential" "bad" {
  method = "snmp"
  params = { community = "public" }
}
`,
				ExpectError: regexp.MustCompile(`set it in secret_params\s+instead`),
			},
		},
	})
}

func TestMockAuthErrors(t *testing.T) {
	requireTerraform(t)
	m := newMockServer(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mockProviderBlock(m, m.viewerToken) + `
resource "shumoku_data_source" "x" {
  name        = "x"
  type        = "zabbix"
  config_json = jsonencode({ url = "https://z" })
}
`,
				ExpectError: regexp.MustCompile(`(?s)Shumoku permission denied.*admin:manage.*Request ID`),
			},
			{
				Config: mockProviderBlock(m, "shmk_wrong") + `
resource "shumoku_topology" "x" {
  name = "x"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Shumoku authentication failed.*did not accept the API\s+token`),
			},
		},
	})
}
