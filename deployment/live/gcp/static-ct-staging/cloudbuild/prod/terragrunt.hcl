locals {
  docker_env = "staging"
  # TODO(phboneff): parametrise this
  base_name  = "arche2025h1" 
}

include "root" {
  path   = find_in_parent_folders()
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
