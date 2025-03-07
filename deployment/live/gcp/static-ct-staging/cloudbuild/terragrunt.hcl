terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//cloudbuild/conformance"
}

locals {
  env          = path_relative_to_include()
  project_id   = get_env("GOOGLE_PROJECT", "static-ct")
  location     = get_env("GOOGLE_REGION", "us-central1")
  base_name    = get_env("TESSERA_BASE_NAME", "${local.env}-cloudbuild")
  github_owner = get_env("GITHUB_OWNER", "transparency-dev")
}

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-${local.base_name}-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}
