variable "snmp_community" {
  type      = string
  sensitive = true
}

resource "shumoku_credential" "campus_snmp" {
  name   = "Campus SNMP"
  method = "snmp"
  secret_params = {
    community = var.snmp_community
  }
  topology_ids = [shumoku_topology.campus.id]
}
