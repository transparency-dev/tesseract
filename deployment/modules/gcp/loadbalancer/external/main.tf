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
  source  = "terraform-google-modules/lb-http/google"
  version = "~> 12.0"
  name    = "tesseract-lb-http"
  project = var.project_id
  // Firewalls are defined externally.
  firewall_networks = []

  create_url_map = false
  url_map        = google_compute_url_map.urlmap.id

  backends = { for log_name in var.log_names :
    "${log_name}-backend" => {
      protocol    = "HTTP"
      port        = 80
      port_name   = "http"
      timeout_sec = 10
      // TODO(phbnf): come back to this
      enable_cdn = false

      health_check = {
        request_path = "/healthz"
        response     = "ok"
        port         = 80
        logging      = true
      }

      // TODO(phbnf): come back to this
      log_config = {
        enable      = true
        sample_rate = 1.0
      }

      groups = [
        {
          // TODO(phbnf): come back to this, set the load balancing mode etc.
          // TODO(phboneff): the region should be set somehow
          group = "projects/${var.project_id}/regions/us-central1/instanceGroups/${log_name}-instance-group-manager"
        },
      ]

      iap_config = {
        enable = false
      }

    }
  }
}

resource "google_compute_url_map" "urlmap" {
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

      // TODO(phboneff): point at json
      default_url_redirect {
        host_redirect          = "transparency.dev"
        path_redirect          = "/"
        https_redirect         = true
        redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
        strip_query            = true
      }

      path_rule {
        paths = [
          "/ct/v1/add-prechain",
          "/ct/v1/add-chain",
          "/ct/v1/get-roots",
        ]
        service = module.gce-lb-http.backend_services["${log_name.value}-backend"].self_link
      }
    }
  }
}
