terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//gce/preloader"
}

locals {
  source_log_uri = "https://ct.googleapis.com/logs/us1/argon2026h1"
}

include "root" {
  path   = find_in_parent_folders("root.hcl")
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)

