terraform {
  required_providers {
    shumoku = {
      source = "shumoku-dev/shumoku"
    }
  }
}

# The endpoint and token can also come from SHUMOKU_ENDPOINT and SHUMOKU_TOKEN.
provider "shumoku" {
  endpoint = "https://shumoku.example.com"
  token    = var.shumoku_token
}

variable "shumoku_token" {
  type      = string
  sensitive = true
}
