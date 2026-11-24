# An attachment is imported as <topology_id>/<attachment_id>. The attachment id
# is the "id" field in GET /api/topologies/<topology_id>/sources.
terraform import shumoku_topology_source.campus_zabbix <topology_id>/<attachment_id>
