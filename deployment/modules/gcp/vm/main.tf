terraform {
  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

# Cloud Run

locals {
  cloudrun_service_account_id = var.env == "" ? "cloudrun-sa" : "cloudrun-${var.env}-sa"
  spanner_log_db_path         = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.log_spanner_db}"
  spanner_antispam_db_path    = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.antispam_spanner_db}"
}

resource "google_project_service" "cloudrun_api" {
  service            = "run.googleapis.com"
  disable_on_destroy = false
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
  description = "This template is used to create TesseraCT instances. Modification test"
  region      = var.location

  lifecycle {
    create_before_destroy = true
  }

  tags = ["thisisatag"]

  labels = {
    environment = var.env
    container-vm = module.gce-container.vm_container_label
  }

  instance_description = "TesseraCT"
  machine_type         = "n2-standard-4"
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
    foo = "foo metadata"
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

#resource "google_compute_region_per_instance_config" "with_disk" {
#  region = var.location
#  region_instance_group_manager = google_compute_region_instance_group_manager.instance_group_manager.name
#  name = "instance-1"
#  preserved_state {
#    metadata = {
#      foo = "bar"
#      // Adding a reference to the instance template used causes the stateful instance to update
#      // if the instance template changes. Otherwise there is no explicit dependency and template
#      // changes may not occur on the stateful instance
#      instance_template = google_compute_region_instance_template.tesseract.self_link
#    }
#  }
#}

// TODO(phbnf): should we use https://github.com/terraform-google-modules/terraform-google-vm/ instead
resource "google_compute_region_instance_group_manager" "instance_group_manager" {
  name               = "${var.base_name}-instance-group-manager"
  region             = var.location

  version {
    instance_template  = google_compute_region_instance_template.tesseract.id
  }

  base_instance_name = var.base_name
  target_size        = "3"

  update_policy {
    type = "PROACTIVE"
    minimal_action = "REFRESH"
  }

  named_port {
    name = "http"
    port = 6962
  }
}

// TODO(phbnf): move to external load balancer, or maybe forward to this one.
module "gce-ilb" {
  source            = "GoogleCloudPlatform/lb-internal/google"
  version           = "~> 7.0"
  region            = var.location
  name              = "${var.base_name}-ilb"
  ports             = ["6962"]
  source_tags       = ["source-tag"]
  target_tags       = ["target-tag"]

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
