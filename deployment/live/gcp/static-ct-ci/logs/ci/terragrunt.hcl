terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/cloudrun"
}

locals {
  env                         = "ci"
  docker_env                  = local.env
  base_name                   = "${local.env}-conformance"
  origin                      = "${local.base_name}.ct.transparency.dev"
  safe_origin                 = replace("${local.origin}", "/[^-a-zA-Z0-9]/", "-")
  log_public_key_secret_name  = "projects/223810646869/secrets/${local.safe_origin}-log-public/versions/1"
  log_private_key_secret_name = "projects/223810646869/secrets/${local.safe_origin}-log-secret/versions/1"
  server_docker_image         = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/conformance-gcp:latest"
  ephemeral                   = true
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
