terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.50.0"
    }
  }

  backend "gcs" {}
}

module "gce-lb-http" {
  source                = "terraform-google-modules/lb-http/google"
  version               = "~> 12.0"
  name                  = "tesseract-lb-http"
  project               = var.project_id
  load_balancing_scheme = "EXTERNAL"
  ssl                   = true
  // Create a single certificate that covers all log domains, and all submission_host_suffixes.
  // Wildcard certificates are not suported.
  managed_ssl_certificate_domains = distinct(flatten([for name, v in var.logs: [v.submission_host_suffix, "${name}.${v.submission_host_suffix}"]]))
  random_certificate_suffix       = true

  // Firewalls are defined externally.
  firewall_networks = []

  create_url_map = false
  url_map        = google_compute_url_map.url_map.id

  // Use the Cloud Armor policy, if it's enabled.
  security_policy = one(module.cloud_armor[*].policy.self_link)

  backends = { for name, v in var.logs:
    "${name}-backend" => {
      protocol    = "HTTP"
      port        = 80
      port_name   = "http"
      timeout_sec = 10
      // TODO(phbnf): Come back to this.
      enable_cdn = false

      health_check = {
        request_path = "/healthz"
        response     = "ok"
        port         = 80
        logging      = true
      }

      // TODO(phbnf): Probabaly a bit too much, but safer to start with.
      log_config = {
        enable      = true
        sample_rate = 1.0
      }

      groups = [
        {
          // A Backend group must have beed deployed independently at this URI.
          group          = "projects/${var.project_id}/regions/${v.region}/instanceGroups/${name}-instance-group-manager"
          balancing_mode = "RATE"
          // Based on the most recent load tests /docs/performance.md
          // Caution:
          //  - The target maximum RPS/QPS can be exceeded if all backends are at or above capacity. 
          //  - Traffic could be routed to instances without going through this load balancer.
          max_rate_per_instance = 1000
        },
      ]

      iap_config = {
        enable = false
      }

    }
  }
}

resource "google_compute_url_map" "url_map" {
  name        = "tesseract-url-map"
  description = "URL map of static-ct-staging logs"

  default_url_redirect {
    host_redirect          = "transparency.dev"
    path_redirect          = "/"
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = true
  }

  dynamic "host_rule" {
    for_each = var.logs
    iterator = log
    content {
      hosts        = ["${log.key}.${log.value.submission_host_suffix}"]
      path_matcher = "${log.key}-path-matcher"
    }
  }

  dynamic "path_matcher" {
    for_each = var.logs
    iterator = log

    content {
      name = "${log.key}-path-matcher"

      // TODO(phboneff): point at json once we have it
      default_url_redirect {
        host_redirect          = "transparency.dev"
        path_redirect          = "/"
        https_redirect         = true
        redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
        strip_query            = true
      }

      path_rule {
        paths = [
          "/ct/v1/add-pre-chain",
          "/ct/v1/add-chain",
          "/ct/v1/get-roots",
        ]
        service = module.gce-lb-http.backend_services["${log.key}-backend"].self_link
      }
    }
  }
}

module "cloud_armor" {
  source  = "GoogleCloudPlatform/cloud-armor/google"
  version = "~> 6.0"

  count                                = var.enable_cloud_armor ? 1 : 0
  project_id                           = var.project_id
  name                                 = "tesseract-security-policy"
  description                          = "TesseraCT LB Security Policy"
  default_rule_action                  = "allow"
  type                                 = "CLOUD_ARMOR"
  layer_7_ddos_defense_enable          = true
  layer_7_ddos_defense_rule_visibility = "STANDARD"
}


