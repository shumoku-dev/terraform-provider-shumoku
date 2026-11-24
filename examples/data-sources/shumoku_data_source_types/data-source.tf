data "shumoku_data_source_types" "all" {}

output "topology_plugins" {
  value = [for t in data.shumoku_data_source_types.all.types : t.type if contains(t.capabilities, "topology")]
}
