terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.43.0"
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
  // Create one cert per log, wildcard certificates are not supported.
  // Put staging.ct.transparency.dev first for it be used as the Common Name.
  managed_ssl_certificate_domains = concat(["staging.ct.transparency.dev"], [for log_name in var.log_names : "${log_name}.staging.ct.transparency.dev"])
  random_certificate_suffix       = true

  // Firewalls are defined externally.
  firewall_networks = []

  create_url_map = false
  url_map        = google_compute_url_map.url_map.id

  backends = { for log_name in var.log_names :
    "${log_name}-backend" => {
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
          group          = "projects/${var.project_id}/regions/${var.log_location}/instanceGroups/${log_name}-instance-group-manager"
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
    for_each = var.log_names
    iterator = log_name
    content {
      hosts        = ["${log_name.value}${var.submission_host_suffix}"]
      path_matcher = "${log_name.value}-path-matcher"
    }
  }

  dynamic "path_matcher" {
    for_each = var.log_names
    iterator = log_name

    content {
      name = "${log_name.value}-path-matcher"

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
        service = module.gce-lb-http.backend_services["${log_name.value}-backend"].self_link
      }
    }
  }
}
