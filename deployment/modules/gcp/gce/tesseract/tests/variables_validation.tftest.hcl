provider "google" {
  project = "dummy-project"
}

variables {
  project_id                                 = "dummy-project"
  base_name                                  = "dummy-base"
  origin                                     = "dummy-origin"
  location                                   = "us-central1"
  env                                        = "dev"
  docker_repo                                = "gcr.io/dummy"
  server_docker_image                        = "dummy-image:latest"
  bucket                                     = "dummy-bucket"
  log_spanner_instance                       = "dummy-instance"
  log_spanner_db                             = "dummy-db"
  antispam_spanner_db                        = "dummy-antispam-db"
  additional_signer_private_key_secret_names = []
  signer_public_key_secret_name              = "projects/dummy/secrets/pub/versions/1"
  signer_private_key_secret_name             = "projects/dummy/secrets/priv/versions/1"
}

run "valid_dates" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00Z"
    not_after_limit = "2024-01-02T00:00:00Z"
  }
}

run "invalid_dates_not_after_start_format" {
  command = plan

  variables {
    not_after_start = "2024-01-01"
  }

  expect_failures = [
    var.not_after_start,
  ]
}

run "invalid_dates_not_after_limit_format" {
  command = plan

  variables {
    not_after_limit = "2024-01-02T00:00:00Z]"
  }

  expect_failures = [
    var.not_after_limit,
  ]
}

run "invalid_dates_start_after_limit" {
  command = plan

  variables {
    not_after_start = "2024-01-02T00:00:00Z"
    not_after_limit = "2024-01-01T00:00:00Z"
  }

  expect_failures = [
    var.not_after_limit,
  ]
}

run "invalid_dates_equal" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00Z"
    not_after_limit = "2024-01-01T00:00:00Z"
  }

  expect_failures = [
    var.not_after_limit,
  ]
}

run "valid_dates_start_empty" {
  command = plan

  variables {
    not_after_start = ""
    not_after_limit = "2024-01-02T00:00:00Z"
  }
}

run "valid_dates_limit_empty" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00Z"
    not_after_limit = ""
  }
}

run "valid_dates_both_empty" {
  command = plan

  variables {
    not_after_start = ""
    not_after_limit = ""
  }
}

run "valid_dates_fractional_seconds" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00.123Z"
    not_after_limit = "2024-01-02T00:00:00.456Z"
  }
}

run "valid_dates_offsets" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00+01:00"
    not_after_limit = "2024-01-02T00:00:00-05:00"
  }
}

run "invalid_dates_not_after_start_lowercase" {
  command = plan

  variables {
    not_after_start = "2024-01-01t00:00:00z"
    not_after_limit = "2024-01-02T00:00:00Z"
  }

  expect_failures = [
    var.not_after_start,
  ]
}

run "invalid_dates_not_after_limit_lowercase" {
  command = plan

  variables {
    not_after_start = "2024-01-01T00:00:00Z"
    not_after_limit = "2024-01-02t00:00:00z"
  }

  expect_failures = [
    var.not_after_limit,
  ]
}

run "valid_num_vms" {
  command = plan

  variables {
    num_vms = 4
  }

  assert {
    condition     = google_compute_region_instance_group_manager.instance_group_manager.target_size == 4
    error_message = "Expected target_size to be 4."
  }
}

run "valid_num_vms_zero" {
  command = plan

  variables {
    num_vms = 0
  }

  assert {
    condition     = google_compute_region_instance_group_manager.instance_group_manager.target_size == 0
    error_message = "Expected target_size to be 0."
  }
}

run "invalid_num_vms_negative" {
  command = plan

  variables {
    num_vms = -1
  }

  expect_failures = [
    var.num_vms,
  ]
}

run "invalid_num_vms_float" {
  command = plan

  variables {
    num_vms = 2.5
  }

  expect_failures = [
    var.num_vms,
  ]
}

run "enable_public_ip_default" {
  command = plan

  assert {
    condition     = length(google_compute_region_instance_template.tesseract.network_interface[0].access_config) == 1
    error_message = "Expected access_config block when enable_public_ip defaults to true."
  }
}

run "enable_public_ip_false" {
  command = plan

  variables {
    enable_public_ip = false
  }

  assert {
    condition     = length(google_compute_region_instance_template.tesseract.network_interface[0].access_config) == 0
    error_message = "Expected no access_config block when enable_public_ip is false."
  }
}

run "mig_defaults" {
  command = plan

  assert {
    condition     = google_compute_region_instance_group_manager.instance_group_manager.name == "dummy-base-instance-group-manager"
    error_message = "Expected the default MIG name to be unsuffixed."
  }

  assert {
    condition     = length(google_compute_region_instance_group_manager.legacy_instance_group_manager) == 0
    error_message = "Expected no legacy MIG by default."
  }
}

run "mig_blue_green" {
  command = plan

  variables {
    mig_suffix        = "-v2"
    retain_legacy_mig = true
  }

  assert {
    condition     = google_compute_region_instance_group_manager.instance_group_manager.name == "dummy-base-instance-group-manager-v2"
    error_message = "Expected the MIG name to carry mig_suffix."
  }

  assert {
    condition     = google_compute_region_instance_group_manager.legacy_instance_group_manager[0].name == "dummy-base-instance-group-manager"
    error_message = "Expected the legacy MIG to keep the unsuffixed name."
  }
}

run "invalid_retain_legacy_mig_without_suffix" {
  command = plan

  variables {
    retain_legacy_mig = true
  }

  expect_failures = [
    var.mig_suffix,
  ]
}
