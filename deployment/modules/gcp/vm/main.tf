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
    image = "us-central1-docker.pkg.dev/static-ct-staging/docker-staging/conformance-gcp@sha256:fd4457a75fd4ccad9678a5e56a659206006deb33f14fc2fb2a727f3ba02c78dc"
    command = "/bin/tesseract-gcp"
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

resource "google_compute_region_instance_template" "tesseract_instance_template_2" {
  name        = "tesseract-template-2"
  description = "This template is used to create TesseraCT instances."
  region      = var.location

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
  }

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email = "${local.cloudrun_service_account_id}@${var.project_id}.iam.gserviceaccount.com" # change this
    scopes = ["cloud-platform"] # come back to this
  }
}

resource "google_compute_region_instance_template" "tesseract_instance_template" {
  name        = "tesseract-template"
  description = "This template is used to create TesseraCT instances."
  region      = var.location

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

resource "google_compute_region_instance_group_manager" "instance_group_manager" {
  name               = "${var.base_name}-instance-group-manager"
  region             = var.location
  version {
    instance_template  = google_compute_region_instance_template.tesseract_instance_template_2.id
  }
  base_instance_name = var.base_name
  target_size        = "3"
  named_port {
    name = "http"
    port = 6962
  }
}

# resource "google_cloud_run_v2_service" "default" {
#   name         = var.base_name
#   location     = var.location
#   launch_stage = "GA"
# 
#   template {
#     service_account                  = "${local.cloudrun_service_account_id}@${var.project_id}.iam.gserviceaccount.com"
#     max_instance_request_concurrency = 1000
#     timeout                          = "30s"
# 
#     scaling {
#       max_instance_count = 2
#       min_instance_count = 2
#     }
# 
#     containers {
#       image = var.server_docker_image
#       name  = "tesseract"
#       args = [
#         "--logtostderr",
#         "--v=3",
#         "--http_endpoint=:6962",
#         "--bucket=${var.bucket}",
#         "--spanner_db_path=${local.spanner_log_db_path}",
#         "--spanner_antispam_db_path=${local.spanner_antispam_db_path}",
#         "--roots_pem_file=/bin/test_root_ca_cert.pem",
#         "--origin=${var.base_name}${var.origin_suffix}",
#         "--signer_public_key_secret_name=${var.signer_public_key_secret_name}",
#         "--signer_private_key_secret_name=${var.signer_private_key_secret_name}",
#       	"--inmemory_antispam_cache_size=250000",
#         "--not_after_start=${var.not_after_start}",
#         "--not_after_limit=${var.not_after_limit}",
#         "--trace_fraction=${var.trace_fraction}",
#         "--batch_max_size=${var.batch_max_size}",
#         "--batch_max_age=${var.batch_max_age}",
#       ]
#       ports {
#         container_port = 6962
#       }
# 
#       resources {
#         limits = {
#           cpu    = "8"
#           memory = "8Gi"
#         }
#       }
# 
#       startup_probe {
#         initial_delay_seconds = 1
#         timeout_seconds       = 1
#         period_seconds        = 10
#         failure_threshold     = 6
#         tcp_socket {
#           port = 6962
#         }
#       }
#     }
#     vpc_access {
#       network_interfaces {
#         network    = "default"
#         subnetwork = "default"
#         tags       = ["thisisatag",]
#       }
#       egress = "ALL_TRAFFIC"
#     }
#   }
# 
#   deletion_protection = false
# 
#   client = "terraform"
# 
#   depends_on = [
#     google_project_service.cloudrun_api,
#   ]
# }
