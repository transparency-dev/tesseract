output "ecdsa_p256_public_key_id" {
  description = "Signer public key (P256_SHA256)"
  value       = module.secretmanager.ecdsa_p256_public_key_id
}

output "ecdsa_p256_public_key_data" {
  description = "Signer public key (P256_SHA256) data from secret manager"
  value       = module.secretmanager.ecdsa_p256_public_key_data
  sensitive   = true
}

output "ecdsa_p256_private_key_id" {
  description = "Signer private key (P256_SHA256)"
  value       = module.secretmanager.ecdsa_p256_private_key_id
}

output "tesseract_url" {
  description = "The submission URL of the running TesseraCT server"
  value       = module.gce.tesseract_url
}

output "tesseract_bucket_name" {
  description = "The GCS bucket name of the TesseraCT log"
  value       = module.storage.log_bucket.name
}
