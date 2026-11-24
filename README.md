# Terraform Provider for Shumoku

Manage a [Shumoku](https://shumoku.dev) Server — topologies, data sources, device
credentials, topology source attachments and dashboards — with Terraform, through
the server's REST API (`/api`).

Registry address: `registry.terraform.io/shumoku-dev/shumoku`.

```hcl
terraform {
  required_providers {
    shumoku = {
      source = "shumoku-dev/shumoku"
    }
  }
}

provider "shumoku" {
  endpoint = "https://shumoku.example.com" # or SHUMOKU_ENDPOINT
  # token  = "shmk_..."                    # or SHUMOKU_TOKEN
}

resource "shumoku_topology" "campus" {
  name = "Campus network"
}

resource "shumoku_data_source" "zabbix" {
  name        = "Zabbix"
  type        = "zabbix"
  config_json = jsonencode({ url = "https://zabbix.example.com", token = var.zabbix_token })
}

resource "shumoku_topology_source" "campus" {
  topology_id    = shumoku_topology.campus.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "topology"
}
```

## Authentication

An administrator issues an API token on the Shumoku Server (for example with
`POST /api/admin/api-tokens`). Tokens look like `shmk_...` and are sent as
`Authorization: Bearer <token>`. The provider checks the token with
`GET /api/auth/status` when it is configured.

Data sources, credentials and topology source attachments are administrator
surfaces (`admin:manage`), so a token that manages them needs the `admin` role.
Topologies and dashboards need `user` (write) access.

## What it manages

| Resource | API | Notes |
| --- | --- | --- |
| `shumoku_topology` | `/api/topologies` | name only; graph content comes from sources and the Editor |
| `shumoku_data_source` | `/api/datasources` | `config_json` is plugin-defined JSON, sensitive |
| `shumoku_credential` | `/api/credentials` | secrets are write-only on the server |
| `shumoku_topology_source` | `/api/topologies/{id}/sources` | import as `<topology_id>/<attachment_id>` |
| `shumoku_dashboard` | `/api/dashboards` | `layout_json` compared semantically |

Data sources: `shumoku_topology` (by id or name), `shumoku_data_source` (by id or
name), `shumoku_data_source_types` (plugin types and their config schemas).

See [`docs/`](docs/) for every attribute and the semantics that matter (server-added
config keys, credential deduplication, write-only secrets).

## Developing

Requirements: Go (see `go.mod`), Terraform CLI.

```sh
make build      # go build ./...
make test       # unit tests + provider tests against an in-memory mock server
make generate   # terraform fmt the examples and regenerate docs/ (tfplugindocs)
make lint
```

`make test` drives the real `terraform` binary (terraform-plugin-testing) against a
mock of the server API in `internal/provider/mockserver_test.go`; no server is needed.

### Acceptance tests

Acceptance tests create and destroy real objects. Run them against a disposable
server with an admin token:

```sh
SHUMOKU_ENDPOINT=http://127.0.0.1:8080 SHUMOKU_TOKEN=shmk_... make testacc
```

### Using a local build

```hcl
# ~/.terraformrc (or a file named by TF_CLI_CONFIG_FILE)
provider_installation {
  dev_overrides {
    "shumoku-dev/shumoku" = "/path/to/go/bin"
  }
  direct {}
}
```

Then `go install .` and run `terraform plan` (no `terraform init` needed).

## Releasing

Push a `v*` tag. The Release workflow builds with GoReleaser, signs the checksums
with GPG (secrets `GPG_PRIVATE_KEY`, `PASSPHRASE`) and publishes a GitHub release
the Terraform Registry picks up. `terraform-registry-manifest.json` declares
protocol 6.0.

## License

Like [Shumoku](https://github.com/konoe-akitoshi/shumoku), this provider is free and
open-source software, licensed under **AGPL-3.0** ([LICENSE](./LICENSE)).

Commercial support is available separately; see Shumoku's
[COMMERCIAL_SUPPORT.md](https://github.com/konoe-akitoshi/shumoku/blob/main/COMMERCIAL_SUPPORT.md).
