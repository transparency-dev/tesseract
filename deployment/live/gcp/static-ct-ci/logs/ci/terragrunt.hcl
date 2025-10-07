terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/cloudrun"
}

locals {
  env                 = "ci"
  docker_env          = local.env
  base_name           = "${local.env}-conformance"
  origin_suffix       = ".ct.transparency.dev"
  server_docker_image = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/conformance-gcp:latest"
  ephemeral           = true
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
