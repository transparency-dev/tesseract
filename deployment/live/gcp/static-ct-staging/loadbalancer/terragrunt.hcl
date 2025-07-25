terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//loadbalancer/external"
}

locals {
  env                    = "staging"
  project_id             = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location               = get_env("GOOGLE_REGION", "us-central1")
  log_names              = []
  submission_host_suffix = ".staging.ct.transparency.dev"
}

inputs = local

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-lb-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}
