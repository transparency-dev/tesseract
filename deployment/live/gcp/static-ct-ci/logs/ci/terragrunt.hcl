terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/cloudrun"
}

locals {
  env                    = "ci"
  docker_env             = local.env
  base_name              = "${local.env}-conformance"
  log_public_key_suffix  = "-ecdsa-p256-public-key"  # Legacy key name pattern.
  log_private_key_suffix = "-ecdsa-p256-private-key" # Legacy key name pattern.
  server_docker_image    = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/conformance-gcp:latest"
  ephemeral              = true
  origin                      = "${local.base_name}.ct.transparency.dev"
  safe_origin                 = replace("${local.origin}", "/[^-a-zA-Z0-9]/", "-")
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
