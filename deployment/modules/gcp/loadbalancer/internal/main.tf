terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.50.0"
    }
  }
}

locals {
  tesseract_url = "http://${var.base_name}.${var.base_name}-ilb.il4.${var.location}.lb.${var.project_id}.internal" // will be created by ilb
}

module "gce_ilb" {
  source      = "GoogleCloudPlatform/lb-internal/google"
  version     = "~> 7.0"
  region      = var.location
  name        = "${var.base_name}-ilb"
  ports       = ["80"]
  source_tags = []
  // TODO(phbnf): come back to this, it doesn't match with the VM tags.
  target_tags                  = []
  service_label                = var.base_name
  create_backend_firewall      = false
  create_health_check_firewall = false

  health_check = {
    type                = "http"
    check_interval_sec  = 1
    healthy_threshold   = 4
    timeout_sec         = 1
    unhealthy_threshold = 5
    response            = ""
    proxy_header        = "NONE"
    port                = 80
    port_name           = "health-check-port"
    request             = ""
    request_path        = "/healthz"
    enable_log          = false
  }

  backends = [
    {
      group          = var.backend_group
      description    = ""
      failover       = false
      balancing_mode = "CONNECTION"
    },
  ]
}
