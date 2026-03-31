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
