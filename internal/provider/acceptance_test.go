package provider

// Acceptance tests against a real Shumoku Server. They run only with TF_ACC=1
// and need SHUMOKU_ENDPOINT and SHUMOKU_TOKEN (an admin API token). They create
// and destroy real objects; point them at a disposable server.
//
//   TF_ACC=1 SHUMOKU_ENDPOINT=http://127.0.0.1:8080 SHUMOKU_TOKEN=shmk_... \
//     go test ./internal/provider -run '^TestAcc' -v

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, env := range []string{envEndpoint, envToken} {
		if os.Getenv(env) == "" {
			t.Fatalf("%s must be set for acceptance tests", env)
		}
	}
}

func TestAccTopologyAndDashboard(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	config := func(suffix string) string {
		return fmt.Sprintf(`
resource "shumoku_topology" "t" {
  name = "%[1]s%[2]s"
}

resource "shumoku_dashboard" "d" {
  name        = "%[1]s%[2]s"
  layout_json = jsonencode({ columns = 12, rowHeight = 100, margin = 8, widgets = [] })
}

data "shumoku_topology" "lookup" {
  id = shumoku_topology.t.id
}
`, name, suffix)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("shumoku_topology.t", "name", name),
					resource.TestCheckResourceAttrPair("data.shumoku_topology.lookup", "name", "shumoku_topology.t", "name"),
					resource.TestCheckResourceAttrSet("shumoku_dashboard.d", "id"),
				),
			},
			{
				Config: config("-renamed"),
				Check:  resource.TestCheckResourceAttr("shumoku_topology.t", "name", name+"-renamed"),
			},
			{ResourceName: "shumoku_topology.t", ImportState: true, ImportStateVerify: true},
			{ResourceName: "shumoku_dashboard.d", ImportState: true, ImportStateVerify: true},
		},
	})
}

// Needs a plugin type that accepts the given config without reaching anything;
// set SHUMOKU_ACC_DS_TYPE / SHUMOKU_ACC_DS_CONFIG to override the Zabbix default
// (pointed at a closed local port; creating a data source does not connect).
func TestAccDataSourceAndAttachment(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	dsType := os.Getenv("SHUMOKU_ACC_DS_TYPE")
	dsConfig := os.Getenv("SHUMOKU_ACC_DS_CONFIG")
	if dsType == "" {
		dsType = "zabbix"
		dsConfig = `{"url":"http://127.0.0.1:9","token":"tf-acc-dummy"}`
	}
	config := func(syncMode string) string {
		return fmt.Sprintf(`
resource "shumoku_topology" "t" {
  name = %[1]q
}

resource "shumoku_data_source" "ds" {
  name        = %[1]q
  type        = %[2]q
  config_json = %[3]q
}

resource "shumoku_topology_source" "s" {
  topology_id    = shumoku_topology.t.id
  data_source_id = shumoku_data_source.ds.id
  purpose        = "metrics"
  sync_mode      = %[4]q
  options_json   = jsonencode({})
}

data "shumoku_data_source_types" "all" {}
`, name, dsType, dsConfig, syncMode)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("manual"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("shumoku_data_source.ds", "id"),
					resource.TestCheckResourceAttr("shumoku_topology_source.s", "sync_mode", "manual"),
				),
			},
			{
				Config: config("webhook"),
				Check:  resource.TestCheckResourceAttrWith("shumoku_topology_source.s", "webhook_secret", nonEmpty),
			},
			{ResourceName: "shumoku_data_source.ds", ImportState: true, ImportStateVerify: true},
			{
				ResourceName:      "shumoku_topology_source.s",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["shumoku_topology_source.s"]
					return rs.Primary.Attributes["topology_id"] + "/" + rs.Primary.ID, nil
				},
			},
		},
	})
}

func TestAccCredential(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	// A random community: the server would link an identical existing credential instead.
	community := acctest.RandString(24)
	config := func(credName, comm string) string {
		return fmt.Sprintf(`
resource "shumoku_topology" "t" {
  name = %[1]q
}

resource "shumoku_credential" "c" {
  name          = %[2]q
  method        = "snmp"
  secret_params = { community = %[3]q }
  topology_ids  = [shumoku_topology.t.id]
}
`, name, credName, comm)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(name, community),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("shumoku_credential.c", "secrets_set.#", "1"),
					resource.TestCheckResourceAttr("shumoku_credential.c", "topology_ids.#", "1"),
				),
			},
			{
				Config: config(name+"-renamed", community+"x"),
				Check:  resource.TestCheckResourceAttr("shumoku_credential.c", "name", name+"-renamed"),
			},
			{
				ResourceName:            "shumoku_credential.c",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret_params"},
			},
		},
	})
}

// Grafana with useWebhook: the server adds a generated webhookSecret to the
// stored config. That must not show as a diff after apply.
func TestAccDataSourceServerGeneratedSecret(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	configFor := func(url string) string {
		return fmt.Sprintf(`
resource "shumoku_data_source" "g" {
  name        = %q
  type        = "grafana"
  config_json = jsonencode({ url = %q, token = "tf-acc-dummy", useWebhook = true })
}

output "webhook_secret" {
  value     = jsondecode(shumoku_data_source.g.effective_config_json).webhookSecret
  sensitive = true
}
`, name, url)
	}
	var secret string
	captureSecret := func(s *terraform.State) error {
		out := s.RootModule().Outputs["webhook_secret"]
		v, _ := out.Value.(string)
		if v == "" {
			return fmt.Errorf("the server generated no webhookSecret")
		}
		if secret != "" && v != secret {
			return fmt.Errorf("webhookSecret changed on update")
		}
		secret = v
		return nil
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configFor("http://127.0.0.1:9"), Check: captureSecret},
			{Config: configFor("http://127.0.0.1:9"), PlanOnly: true},
			// An update must not make the server regenerate the secret.
			{Config: configFor("http://127.0.0.1:10"), Check: captureSecret},
		},
	})
}

// Creating a credential identical to an existing one is refused (the server
// would link the existing one), and the links the server added are undone.
func TestAccCredentialLinkedExisting(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	community := acctest.RandString(24)
	base := fmt.Sprintf(`
resource "shumoku_topology" "a" {
  name = "%[1]s-a"
}

resource "shumoku_topology" "b" {
  name = "%[1]s-b"
}

resource "shumoku_credential" "first" {
  name          = "%[1]s-first"
  method        = "snmp"
  secret_params = { community = %[2]q }
  topology_ids  = [shumoku_topology.a.id]
}
`, name, community)
	dupe := base + fmt.Sprintf(`
resource "shumoku_credential" "dupe" {
  name          = "%[1]s-dupe"
  method        = "snmp"
  secret_params = { community = %[2]q }
  topology_ids  = [shumoku_topology.b.id]
}
`, name, community)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: base},
			{Config: dupe, ExpectError: regexp.MustCompile(`Identical credential already exists`)},
			// The rollback restored the first credential's links: no diff on it.
			{
				Config: base,
				Check:  resource.TestCheckResourceAttr("shumoku_credential.first", "topology_ids.#", "1"),
			},
			{Config: base, PlanOnly: true},
		},
	})
}

func TestAccInvalidToken(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "shumoku" {
  token = "shmk_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}

data "shumoku_data_source_types" "all" {}
`,
				ExpectError: regexp.MustCompile(`Shumoku authentication failed`),
			},
		},
	})
}
