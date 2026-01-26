terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "6.50.0"
    }
  }
}

locals {
  # TODO(phbnf): use a different service account
  tesseract_service_account_id = var.env == "" ? "tesseract-sa" : "tesseract-${var.env}-sa"
  spanner_log_db_path          = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.log_spanner_db}"
  spanner_antispam_db_path     = "projects/${var.project_id}/instances/${var.log_spanner_instance}/databases/${var.antispam_spanner_db}"
}

data "google_compute_image" "cos" {
  family  = "cos-121-lts"
  project = "cos-cloud"
}

locals {

  witness_policy_file = "/etc/tesseract-witness.policy"
  accepted_roots_file = "/etc/roots.pem"

  # docker_run_args are provided to the docker run command.
  # Use this to configure docker-specific things.
  docker_run_args = join(" ", compact([
    # Map the port
    "-p 80:80",
    # Ensure that TesseraCT logs are delivered to GCP logging.
    "--log-driver=gcplogs",
    # Bind-mount the witness policy, if one has been provided.
    var.witness_policy == "" ? "" : "--mount type=bind,src=${local.witness_policy_file},dst=${local.witness_policy_file}",
    # Bind-mount the roots file, if one has been provided.
    var.accepted_roots == "" ? "" : "--mount type=bind,src=${local.accepted_roots_file},dst=${local.accepted_roots_file}"
  ]))

  # tesseract_args are provided to the tesseract command.
  tesseract_args = join(" ", [
    "-logtostderr",
    "-v=2",
    "-http_endpoint=:80",
    "-bucket=${var.bucket}",
    "-spanner_db_path=${local.spanner_log_db_path}",
    "-spanner_antispam_db_path=${local.spanner_antispam_db_path}",
    format("-roots_pem_file=%s", var.accepted_roots == "" ? "/bin/test_root_ca_cert.pem" : local.accepted_roots_file),
    "-origin=${var.origin}",
    "-signer_public_key_secret_name=${var.signer_public_key_secret_name}",
    "-signer_private_key_secret_name=${var.signer_private_key_secret_name}",
    "-inmemory_antispam_cache_size=256k",
    "-not_after_start=${var.not_after_start}",
    "-not_after_limit=${var.not_after_limit}",
    "-trace_fraction=${var.trace_fraction}",
    "-batch_max_size=${var.batch_max_size}",
    "-batch_max_age=${var.batch_max_age}",
    "-enable_publication_awaiter=${var.enable_publication_awaiter}",
    "-accept_sha1_signing_algorithms=true",
    "-rate_limit_old_not_before=${var.rate_limit_old_not_before}",
    "-rate_limit_dedup=${var.rate_limit_dedup}",
    "-roots_remote_fetch_url=${var.roots_remote_fetch_url}",
    "-roots_remote_fetch_interval=${var.roots_remote_fetch_interval}",
    var.witness_policy == "" ? "" : "-witness_policy_file=${local.witness_policy_file}",
    length(var.additional_signer_private_key_secret_names) == 0 ? "" : join(" ", formatlist("-additional_signer_private_key_secret_name=%s", var.additional_signer_private_key_secret_names))
  ])

  container_name = "tesseract-${var.base_name}"

  // cloud_init is the config used to configure the COS VM.
  //
  // See:
  // - Cloud Init docs: https://cloudinit.readthedocs.io/en/latest/index.html
  // - Systemd config docs: https://www.freedesktop.org/software/systemd/man/latest/systemd.directives.html
  cloud_init = <<EOT
    #cloud-config

    users:
      - name: tesseract
        uid: 2000
        groups: docker # Add the user to the Docker group

    write_files:
      - path: ${local.witness_policy_file}
        permissions: 0444
        owner: root
        encoding: b64
        content: ${base64encode(var.witness_policy)}
      - path: ${local.accepted_roots_file}
        permissions: 0444
        owner: root
        encoding: b64
        content: ${base64encode(var.accepted_roots)}
      - path: /etc/systemd/system/config-firewall.service
        permissions: 0644
        owner: root
        content: |
          [Unit]
          Description=Configures the host firewall
          
          [Service]
          Type=oneshot
          RemainAfterExit=true
          ExecStart=/sbin/iptables -A INPUT -p tcp --dport 80 -j ACCEPT
      - path: /etc/systemd/system/tesseract.service
        permissions: 0644
        owner: root
        content: |
          [Unit]
          Description=Run TesseraCT
          Wants=gcr-online.target docker.socket config-firewall.service
          After=gcr-online.target docker.socket config-firewall.service

          [Service]
          ExecStartPre=sudo -u tesseract /usr/bin/docker-credential-gcr configure-docker --registries ${var.location}-docker.pkg.dev
          ExecStart=sudo -u tesseract -E /usr/bin/docker run --rm -u 2000 --name=${local.container_name} ${local.docker_run_args} ${var.server_docker_image} ${local.tesseract_args}
          ExecStop=sudo -u tesseract /usr/bin/docker stop ${local.container_name}
          ExecStopPost=sudo -u /usr/bin/docker rm ${local.container_name}
          StandardOutput=journal
          StandardError=journal

    runcmd:
      - systemctl daemon-reload
      - systemctl start tesseract.service
    EOT
}

resource "google_compute_region_instance_template" "tesseract" {
  // Templates cannot be updated, so we generate a new one every time.
  name_prefix = "tesseract-template-"
  description = "This template is used to create TesseraCT instances."
  region      = var.location

  lifecycle {
    create_before_destroy = true
  }

  tags = ["tesseract", "allow-health-checks", "preloader"]

  labels = {
    environment = var.env
  }

  instance_description = "TesseraCT"
  machine_type         = var.machine_type

  scheduling {
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
  }

  // Create a new boot disk from an image
  disk {
    source_image = data.google_compute_image.cos.self_link
    auto_delete  = true
    boot         = true
  }

  network_interface {
    network = "default"
    access_config {
      // Ephemeral public IP for outbound witness requests.
    }
  }

  metadata = {
    google-logging-enabled    = "true"
    google-monitoring-enabled = "true"
    user-data                 = local.cloud_init
  }

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email  = "${local.tesseract_service_account_id}@${var.project_id}.iam.gserviceaccount.com"
    scopes = ["cloud-platform"] # Allows using service accounts and OAuth.
  }
}

resource "google_compute_health_check" "healthz" {
  count               = var.health_checks ? 1: 0
  name                = "${var.base_name}-mig-hc-http"
  timeout_sec         = 10
  check_interval_sec  = 10
  healthy_threshold   = 1
  unhealthy_threshold = 5

  http_health_check {
    request_path = "/healthz"
    response     = "ok"
    port         = 80
  }

  log_config {
    enable = true
  }
}

resource "google_compute_region_instance_group_manager" "instance_group_manager" {
  name   = "${var.base_name}-instance-group-manager"
  region = var.location

  version {
    instance_template = google_compute_region_instance_template.tesseract.id
  }
  wait_for_instances        = true
  wait_for_instances_status = "UPDATED"

  all_instances_config {
    metadata = {
      service_name = var.base_name
    }
    labels = {
      service_name = var.base_name
    }
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
    replacement_method    = "SUBSTITUTE"
    max_surge_fixed       = 3 // must be greater or equal than the number of zones, which itself default to 3
    max_unavailable_fixed = 0 // wait for new VMs to be up before turning down the old ones
  }

  named_port {
    name = "http"
    port = 80
  }


  dynamic "auto_healing_policies" {
    for_each = google_compute_health_check.healthz[*]

    content {
      health_check      = auto_healing_policies.value.id
      initial_delay_sec = 300 // Give enough time for the TesseraCT container to start.
    }
  }
}
