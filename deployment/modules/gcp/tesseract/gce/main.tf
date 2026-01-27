terraform {
  backend "gcs" {}
}

module "storage" {
  source = "../../storage"

  project_id    = var.project_id
  bucket_name   = "${var.base_name}${var.origin_suffix}"
  base_name     = var.base_name
  location      = var.location
  ephemeral     = var.ephemeral
  spanner_pu    = var.spanner_pu
  public_bucket = var.public_bucket
}

module "gce" {
  source = "../../gce/tesseract"

  env                                        = var.env
  project_id                                 = var.project_id
  base_name                                  = var.base_name
  origin                                     = var.origin
  location                                   = var.location
  server_docker_image                        = var.server_docker_image
  machine_type                               = var.machine_type
  not_after_start                            = var.not_after_start
  not_after_limit                            = var.not_after_limit
  bucket                                     = module.storage.log_bucket.id
  log_spanner_instance                       = module.storage.log_spanner_instance.name
  log_spanner_db                             = module.storage.log_spanner_db.name
  antispam_spanner_db                        = module.storage.antispam_spanner_db.name
  signer_public_key_secret_name              = var.log_public_key_secret_name
  signer_private_key_secret_name             = var.log_private_key_secret_name
  additional_signer_private_key_secret_names = var.additional_signer_private_key_secret_names
  trace_fraction                             = var.trace_fraction
  batch_max_age                              = var.batch_max_age
  batch_max_size                             = var.batch_max_size
  enable_publication_awaiter                 = var.enable_publication_awaiter
  rate_limit_old_not_before                  = var.rate_limit_old_not_before
  rate_limit_dedup                           = var.rate_limit_dedup
  witness_policy                             = var.witness_policy
  accepted_roots                             = var.accepted_roots
  health_checks                              = var.gce_health_checks
  roots_remote_fetch_url                     = var.roots_remote_fetch_url
  roots_remote_fetch_interval                = var.roots_remote_fetch_interval

  depends_on = [
    module.storage
  ]
}

module "ilb" {
  source = "../../loadbalancer/internal"

  env           = var.env
  project_id    = var.project_id
  base_name     = var.base_name
  location      = var.location
  backend_group = module.gce.instance_group

  depends_on = [
    module.gce,
  ]
}
