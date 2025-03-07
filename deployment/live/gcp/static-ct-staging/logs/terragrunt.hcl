locals {
  project_id     = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location       = get_env("GOOGLE_REGION", "us-central1")
  base_name      = path_relative_to_include()
  origin_suffix  = get_env("TESSERA_ORIGIN_SUFFIX", "")
  github_owner   = get_env("GITHUB_OWNER", "phbnf")
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
      env  = "${local.base_name}"
    }
  }
}
