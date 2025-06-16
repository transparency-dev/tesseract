terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/cloudrun"
}

locals {
  env                 = include.root.locals.env
  docker_env          = local.env
  base_name           = include.root.locals.base_name
  origin_suffix       = include.root.locals.origin_suffix
  not_after_start     = "2025-01-01T00:00:00Z"
  not_after_limit     = "2025-07-01T00:00:00Z"
  server_docker_image = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/tesseract-gcp:${include.root.locals.docker_container_tag}"
  spanner_pu          = 500
  trace_fraction      = 0.1
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
