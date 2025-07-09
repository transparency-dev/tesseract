terraform {
  backend "gcs" {}
  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

locals {
  cloudrun_service_account_id = var.env == "" ? "cloudrun-sa" : "cloudrun-${var.env}-sa"
}

module "gce_container_preloader" {
  # https://github.com/terraform-google-modules/terraform-google-container-vm
  source = "terraform-google-modules/container-vm/google"
  version = "~> 2.0"

  container = {
    image = var.server_docker_image,
    args = [
      "--target_log_uri=${var.target_log_uri}",
      "--source_log_uri=${var.source_log_uri}",
      "--start_index=${var.start_index}",
      "--continuous=true",
      "--num_workers=500", 
      "--parallel_fetch=20", 
      "--parallel_submit=1500",
      "--v=2",
    ]
    tty : true # maybe remove this
  }

  restart_policy = "Always"
}

resource "google_compute_instance" "preloader" {
  name         = "${var.base_name}-preloader"
  machine_type = var.machine_type
  zone         = "${var.location}-f"

  tags = ["preloader-allow-group"]

  boot_disk {
    initialize_params {
      image  = module.gce_container_preloader.source_image
      labels = {
        my_label = "value"
      }
    }
  }

  network_interface {
    network = "default"
  }

  labels = {
    environment = var.env
    container-vm = module.gce_container_preloader.vm_container_label
  }

  # Come back to this: the start_index needs to be manually edited
  # when the prelaoder restarts.
  scheduling {
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
  }
  allow_stopping_for_update =  true

  metadata = {
    gce-container-declaration = module.gce_container_preloader.metadata_value
    google-logging-enabled = "true"
    google-monitoring-enabled = "true"
  }

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email = "${local.cloudrun_service_account_id}@${var.project_id}.iam.gserviceaccount.com" # TODO(phbnf): change this
    scopes = ["cloud-platform"] # Allows using service accounts and OAuth.
  }
}
