terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//cloudbuild/tesseract"
}

locals {
  env              = get_env("GOOGLE_ENV", "staging")
  docker_env       = get_env("GOOGLE_ENV", "staging")
  project_id       = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location         = get_env("GOOGLE_REGION", "us-central1")
  github_owner     = get_env("GITHUB_OWNER", "transparency-dev")
  logs_terragrunts = ["deployment/live/gcp/static-ct-staging/logs/arche2025h1",]
}

inputs = local

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-cloudbuild-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}

