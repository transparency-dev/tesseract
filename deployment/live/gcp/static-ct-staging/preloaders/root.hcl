locals {
  env                 = "staging"
  project_id          = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location            = get_env("GOOGLE_REGION", "us-central1")
  base_name           = path_relative_to_include()
  github_owner        = get_env("GITHUB_OWNER", "transparency-dev")
  target_log_uri      = get_env("TARGET_LOG_URI", "http://${local.base_name}.${local.base_name}-ilb.il4.${local.location}.lb.${local.project_id}.internal:6962/${local.base_name}.staging.ct.transparency.dev/")
  server_docker_image = "us-central1-docker.pkg.dev/static-ct-staging/docker-staging/preloader:latest"
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
