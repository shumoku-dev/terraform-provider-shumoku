resource "shumoku_topology_source" "campus_zabbix" {
  topology_id    = shumoku_topology.campus.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "topology"
  sync_mode      = "on_view"
}

resource "shumoku_topology_source" "campus_metrics" {
  topology_id    = shumoku_topology.campus.id
  data_source_id = shumoku_data_source.zabbix.id
  purpose        = "metrics"
}
