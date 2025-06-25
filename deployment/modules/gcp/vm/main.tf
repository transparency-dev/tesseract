terraform {
  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

locals {
  # TODO(phbnf): use a different service account
  cloudrun_service_account_id = var.env == "" ? "cloudrun-sa" : "cloudrun-${var.env}-sa"
  spanner_log_db_path         = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.log_spanner_db}"
  spanner_antispam_db_path    = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.antispam_spanner_db}"
  tesseract_url               = "http://${var.base_name}.${var.base_name}-ilb.il4.${var.location}.lb.${var.project_id}.internal" // will be created by ilb
}

module "gce-container" {
  # https://github.com/terraform-google-modules/terraform-google-container-vm
  source = "terraform-google-modules/container-vm/google"
  version = "~> 2.0"

  container = {
    image = var.server_docker_image
    args = [
      "--logtostderr",
      "--v=3",
      "--http_endpoint=:6962",
      "--bucket=${var.bucket}",
      "--spanner_db_path=${local.spanner_log_db_path}",
      "--spanner_antispam_db_path=${local.spanner_antispam_db_path}",
      "--roots_pem_file=/bin/test_root_ca_cert.pem",
      "--origin=${var.base_name}${var.origin_suffix}",
      "--signer_public_key_secret_name=${var.signer_public_key_secret_name}",
      "--signer_private_key_secret_name=${var.signer_private_key_secret_name}",
      "--inmemory_antispam_cache_size=256k",
      "--not_after_start=${var.not_after_start}",
      "--not_after_limit=${var.not_after_limit}",
      "--trace_fraction=${var.trace_fraction}",
      "--batch_max_size=${var.batch_max_size}",
      "--batch_max_age=${var.batch_max_age}",
    ]
    tty : true # maybe remove this
  }

  restart_policy = "Always"
}

resource "random_string" "random" {
  length           = 6
  lower            = true
  upper            = false
  special          = false
}

resource "google_compute_region_instance_template" "tesseract" {
  // Templates cannot be updated, so we generate a new one every time.
  name_prefix = "tesseract-template-"
  description = "This template is used to create TesseraCT instances."
  region      = var.location

  lifecycle {
    create_before_destroy = true
  }

  tags = ["tesseract-allow-group"]

  labels = {
    environment = var.env
    container-vm = module.gce-container.vm_container_label
  }

  instance_description = "TesseraCT"
  machine_type         = var.machine_type
  can_ip_forward       = false # come back to this

  scheduling {
    automatic_restart   = true # come back to this
    on_host_maintenance = "MIGRATE" # come back to his
  }

  // Create a new boot disk from an image
  disk {
    source_image      = module.gce-container.source_image # come back to this
    auto_delete       = true
    boot              = true
  }

  network_interface {
    network = "default"
  }

  metadata = {
    gce-container-declaration = module.gce-container.metadata_value
    google-logging-enabled = "true"
    google-monitoring-enabled = "true"
  }

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email = "${local.cloudrun_service_account_id}@${var.project_id}.iam.gserviceaccount.com" # change this
    scopes = ["cloud-platform"] # come back to this
  }
}

resource "google_compute_health_check" "healthz" {
  name                = "${var.base_name}-health-check"
  timeout_sec         = 10
  check_interval_sec  = 30
  healthy_threshold   = 1
  unhealthy_threshold = 3
  
  http_health_check {
    request_path = "/healthz"
    response     = "ok"
    port         = 6962
  }
}

resource "google_compute_region_instance_group_manager" "instance_group_manager" {
  name               = "${var.base_name}-instance-group-manager"
  region             = var.location

  version {
    instance_template  = google_compute_region_instance_template.tesseract.id
  }

  base_instance_name = var.base_name
  target_size        = "3"

  update_policy {
    type                           = "PROACTIVE"
    instance_redistribution_type   = "PROACTIVE"
    minimal_action                 = "REPLACE"
    most_disruptive_allowed_action = "REPLACE"
    # TODO(phbnf): come back to this, it's a beta feature for now
    # min_ready_sec                  = 50
    replacement_method             = "SUBSTITUTE"
  }

  named_port {
    name = "http"
    port = 6962
  }
  
# TODO(phbnf): re-enable this once we have approval to have custom firewall allowing these probes.
#   auto_healing_policies {
#     health_check      = google_compute_health_check.healthz.id
#     initial_delay_sec = 90 // Give enough time for the TesseraCT container to start.
#   }
}

// TODO(phbnf): move to external load balancer, or maybe forward to this one.
module "gce-ilb" {
  source            = "GoogleCloudPlatform/lb-internal/google"
  version           = "~> 7.0"
  region            = var.location
  name              = "${var.base_name}-ilb"
  ports             = ["6962"]
  source_tags       = []
  target_tags       = ["${var.base_name}-allow-group"]
  service_label     = var.base_name

  health_check = {
    type                = "http"
    check_interval_sec  = 1
    healthy_threshold   = 4
    timeout_sec         = 1
    unhealthy_threshold = 5
    response            = ""
    proxy_header        = "NONE"
    port                = 6962
    port_name           = "health-check-port"
    request             = ""
    request_path        = "/healthz"
    host                = "1.2.3.4"
    enable_log          = false
  }

  backends = [
    {
      group       = google_compute_region_instance_group_manager.instance_group_manager.instance_group
      description = ""
      failover    = false
      balancing_mode = "CONNECTION"
    },
  ]
}
