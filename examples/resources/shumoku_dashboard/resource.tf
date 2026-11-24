resource "shumoku_dashboard" "noc" {
  name = "NOC"
  layout_json = jsonencode({
    columns   = 12
    rowHeight = 100
    margin    = 8
    widgets   = []
  })
}
