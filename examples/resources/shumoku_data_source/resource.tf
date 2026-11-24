variable "zabbix_token" {
  type      = string
  sensitive = true
}

variable "grafana_token" {
  type      = string
  sensitive = true
}

# config_json follows the plugin's configSchema
# (see the shumoku_data_source_types data source).
resource "shumoku_data_source" "zabbix" {
  name = "Zabbix"
  type = "zabbix"
  config_json = jsonencode({
    url   = "https://zabbix.example.com"
    token = var.zabbix_token
  })

  # Optional per-source polling overrides, in milliseconds.
  polling_alerts_ms = 30000
}

# Grafana with webhook alerts: the server generates the webhook secret.
resource "shumoku_data_source" "grafana" {
  name = "Grafana"
  type = "grafana"
  config_json = jsonencode({
    url        = "https://grafana.example.com"
    token      = var.grafana_token
    useWebhook = true
  })
}

output "grafana_webhook_secret" {
  value     = jsondecode(shumoku_data_source.grafana.effective_config_json).webhookSecret
  sensitive = true
}
