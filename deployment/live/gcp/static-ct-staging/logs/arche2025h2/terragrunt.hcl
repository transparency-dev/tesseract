terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/gce"
}

locals {
  env                                        = include.root.locals.env
  docker_env                                 = local.env
  base_name                                  = include.root.locals.base_name
  origin_suffix                              = include.root.locals.origin_suffix
  not_after_start                            = "2025-07-01T00:00:00Z"
  not_after_limit                            = "2026-01-01T00:00:00Z"
  server_docker_image                        = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/tesseract-gcp:${include.root.locals.docker_container_tag}"
  spanner_pu                                 = 500
  trace_fraction                             = 0.1
  create_internal_load_balancer              = true
  public_bucket                              = true
  machine_type                               = "n2-standard-8"
  rate_limit_old_not_before                  = "28h:150"
  additional_signer_private_key_secret_names = ["projects/781477119959/secrets/arche2025h2-ed25519-private-key/versions/1"]
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
