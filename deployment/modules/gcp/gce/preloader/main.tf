terraform {
  backend "gcs" {}
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.50.0"
    }
  }
}

locals {
  preloader_service_account_id = var.env == "" ? "preloader-sa" : "preloader-${var.env}-sa"

  preloader_container_name = "preloader-${var.env}"

  # docker_run_args are provided to the docker run command.
  # Use this to configure docker-specific things.
  docker_run_args = join(" ", compact([
    # Ensure that TesseraCT logs are delivered to GCP logging.
    "--log-driver=gcplogs",
  ]))

  # Args for the preloader command.
  preloader_args = join(" ", [
    "--target_log_uri=${var.target_log_uri}",
    "--source_log_uri=${var.source_log_uri}",
    "--start_index=${var.start_index}",
    "--continuous=true",
    "--num_workers=500",
    "--parallel_fetch=20",
    "--parallel_submit=1500",
    "--v=2",
  ])

  preloader_cloud_init = <<EOT
    #cloud-config

    users:
      - name: preloader
        uid: 2000
        groups: docker # Add the user to the Docker group

    write_files:
      - path: /etc/systemd/system/preloader.service
        permissions: 0644
        owner: root
        content: |
          [Unit]
          Description=Run Preloader

          [Service]
          ExecStartPre=sudo -u preloader /usr/bin/docker-credential-gcr configure-docker --registries ${var.location}-docker.pkg.dev
          ExecStart=sudo -u preloader -E /usr/bin/docker run --rm -u 2000 --name=${local.preloader_container_name} ${local.docker_run_args} ${var.server_docker_image} ${local.preloader_args}
          ExecStop=sudo -u preloader /usr/bin/docker stop ${local.preloader_container_name}
          ExecStopPost=sudo -u /usr/bin/docker rm ${local.preloader_container_name}
          StandardOutput=journal
          StandardError=journal

    runcmd:
      - systemctl daemon-reload
      - systemctl start preloader.service
    EOT

}

data "google_compute_image" "cos" {
  family  = "cos-121-lts"
  project = "cos-cloud"
}

resource "google_compute_instance" "preloader" {
  name         = "${var.base_name}-preloader"
  machine_type = var.machine_type
  zone         = "${var.location}-f"

  tags = ["preloader"]

  network_interface {
    network = "default"
    access_config {
      // Ephemeral public IP for outbound witness requests.
    }
  }

  boot_disk {
    initialize_params {
      image = data.google_compute_image.cos.self_link
    }
  }

  labels = {
    environment = var.env
  }

  # Come back to this: the start_index needs to be manually edited
  # when the prelaoder restarts.
  scheduling {
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
  }
  allow_stopping_for_update = true

  metadata = {
    google-logging-enabled    = "true"
    google-monitoring-enabled = "true"
    user-data                 = local.preloader_cloud_init
  }

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email  = "${local.preloader_service_account_id}@${var.project_id}.iam.gserviceaccount.com"
    scopes = ["cloud-platform"] # Allows using service accounts and OAuth.
  }
}
