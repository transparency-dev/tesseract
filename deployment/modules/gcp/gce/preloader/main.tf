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
}


module "gce_container_preloader" {
  # https://github.com/terraform-google-modules/terraform-google-container-vm
  source = "terraform-google-modules/container-vm/google"
  version = "~> 2.0"

  container = {
    image = "us-central1-docker.pkg.dev/static-ct-staging/docker-staging/preloader@sha256:4fd99df0ba68b726cef52d41c05a2e58dbd077ee4eddd7396e871a91caa46394"
    args = [
      "--target_log_uri=${var.target_log_uri}:6962/${var.base_name}${var.origin_suffix}", // TODO(phbnf): put the full URL in the var and get rid of origin suffix
      "--source_log_uri=${var.source_log_uri}",
      "--start_index=${var.start_index}",
      "--num_workers=500", 
      "--parallel_fetch=20", 
      "--parallel_submit=500",
    ]
    tty : true # maybe remove this
  }

  restart_policy = "Always"
}

resource "google_compute_instance" "preloader" {
  name         = "${var.base_name}-preloader"
  machine_type = "n2-standard-4"
  zone         = "us-central1-f"

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
    email = "${local.cloudrun_service_account_id}@${var.project_id}.iam.gserviceaccount.com" # change this
    scopes = ["cloud-platform"] # Allows using service accounts and OAuth.
  }
}
