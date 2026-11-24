# Attach a data source that is managed outside Terraform.
data "shumoku_data_source" "netbox" {
  name = "NetBox"
}

resource "shumoku_topology_source" "netbox" {
  topology_id    = data.shumoku_topology.campus.id
  data_source_id = data.shumoku_data_source.netbox.id
  purpose        = "topology"
}
