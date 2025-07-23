locals {
  env            = get_env("GOOGLE_ENV", "staging")
  project_id     = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location       = get_env("GOOGLE_REGION", "us-central1")
  base_name      = path_relative_to_include()
  github_owner   = get_env("GITHUB_OWNER", "transparency-dev")
  submission_url = get_env("SUBMISSION_URL", "https://${local.base_name}-64t3hlisgq-uc.a.run.app/")
  monitoring_url = get_env("MONITORING_URL", " https://storage.googleapis.com/static-ct-staging-${local.base_name}-bucket")
}

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-preloader-${local.base_name}-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}
