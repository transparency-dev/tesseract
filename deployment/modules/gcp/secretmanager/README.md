# Secret Manager

This module was removed since the tesseract terraform config no-longer creates secret keys for logs as this cannot be done safely.

Instead, operators are expected to create the keys prior to turning up a log. The [generate_key](/cmd/tesseract/gcp/generate_key)
tool can be used to do this.

> [!WARNING]
> If you used this terraform to create a TesseraCT/GCP instance prior to the Secret Manager module being removed, terraform
> will want to delete these resources when this updated config is applied. If you proceed, it will DELETE your log's keys.
> 
> To avoid this, please use the [`terragrunt state rm`](https://developer.hashicorp.com/terraform/cli/commands/state/rm) command to
> stop terraform from attempting the manage them, while leaving them intact in GCP.

