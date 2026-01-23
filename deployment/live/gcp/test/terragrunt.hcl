terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/test"
}

locals {
  env       = "test"
  base_name = get_env("TESSERA_BASE_NAME", "${local.env}-static-ct")
  ephemeral = true # This is a "throwaway" test log, so don't mark resources as delete protected.
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
